import { store } from '../../utils/store';
import { gameSocket, DEFAULT_WS_URL } from '../../utils/socket';
import { wireToTable } from '../../utils/wire-adapter';
import { SUIT_SYMBOL, SUIT_COLOR, handRankLabel } from '../../utils/cards';
import { ensureLoggedIn } from '../../utils/auth';
import * as sfx from '../../utils/sfx';
import { translateServerError } from '../../utils/error-i18n';
import type {
  Card,
  ChatMessagePayload,
  ErrorPayload,
  GameEndedPayload,
  GameEventPayload,
  GameStage,
  JoinedPayload,
  Player,
  PlayerAction,
  PlayerSettlement,
  ServerMessage,
  Suit,
  TableState,
  WireRoomState,
} from '../../types/game';

// Opponent slot positions around the oval felt, indexed by posKey =
// (theirSeat - mySeat + maxSeats) % maxSeats. posKey 0 is "me" and rendered
// in the bottom me-zone, so only 1..8 appear here (max 9-seat table).
const OPPONENT_POSITIONS_9: Record<number, { top: string; left: string }> = {
  1: { top: '78%', left: '84%' },
  2: { top: '50%', left: '90%' },
  3: { top: '22%', left: '84%' },
  4: { top: '8%',  left: '66%' },
  5: { top: '6%',  left: '50%' },
  6: { top: '8%',  left: '34%' },
  7: { top: '22%', left: '16%' },
  8: { top: '50%', left: '10%' },
};

interface MyCardView {
  suit: Suit | '';
  rank: string;
  symbol: string;
  color: 'red' | 'black';
}

interface OpponentView extends Player {
  posKey: number; // 1..8, relative to my seat clockwise (9-seat table)
  handRank: string; // Chinese label of evaluated hand at showdown; '' otherwise
}

// All overlay floaters carry a `hidden` flag so we can mark them done via a
// path-syntax setData (single-bool write) instead of cloning + filtering the
// whole array on each cleanup. The arrays themselves get a one-shot reset at
// hand-boundary transitions (see applyWireState resolving→non-resolving guard).
// chip-fly's from/to use px offsets from felt centre so the keyframes can run
// on transform (compositor) instead of animating top/left (layout+paint).
// chat/win bubbles keep % anchors because their keyframes already animate
// transform-only — moving the anchor wouldn't help.
interface ChipFly {
  id: number;
  fromX: number;   // px offset from felt centre
  fromY: number;
  toX: number;
  toY: number;
  amount: number;
  hidden: boolean;
}

interface ChatBubble {
  id: number;
  seat: number;
  top: string;
  left: string;
  emoji: string;
  hidden: boolean;
}

interface WinBubble {
  id: number;
  seat: number;
  top: string;
  left: string;
  amount: number;
  hidden: boolean;
}

interface DealCardAnim {
  id: number;
  isMe: boolean;
  dx: number;      // px offset from felt centre (0 when isMe — handled by dy only)
  dy: number;
  delay: number;   // ms, drives sequential fan-out
}

const DEAL_STAGGER_MS = 120;
const DEAL_CARD_ANIM_MS = 450;

const STAGE_LABEL: Record<GameStage, string> = {
  waiting: '等待中',
  preflop: '翻前',
  flop: '翻牌',
  turn: '转牌',
  river: '河牌',
  showdown: '摊牌',
  'hand-complete': '本手结束',
};

const QUICK_EMOJIS = ['👍', '👏', '😂', '😱', '🤔', '🔥', '💪', '🃏'];

interface PageData {
  table: TableState | null;
  opponents: OpponentView[];
  myPlayer: Player | null;
  myCardsView: MyCardView[];
  opponentPositions: Record<number, { top: string; left: string }>;
  isMyTurn: boolean;
  callAmount: number;
  canCheck: boolean;
  statusBarHeight: number;
  connectionState: 'idle' | 'connecting' | 'connected' | 'disconnected';
  banner: string;

  // Game-duration countdown (only shown when endsAt > 0)
  countdownText: string;
  endingHint: string;
  stageLabel: string;

  // End-of-game settlement panel
  settlementVisible: boolean;
  settlementPlayers: PlayerSettlement[];

  // Animations
  isDealing: boolean;            // hole card deal animation playing
  dealCards: DealCardAnim[];     // pre-computed sequential deal placements
  newCommunityIndices: number[]; // indices of community cards just revealed (for flip-in)
  chipFlies: ChipFly[];          // active chip-to-pot animations
  chatBubbles: ChatBubble[];     // active floating chat bubbles
  winBubbles: WinBubble[];       // active "+N" pot-win floats above winner avatars
  chatPickerOpen: boolean;
  quickEmojis: string[];

  // True while a hand is being dealt/played — gates the face-down hole-card
  // backs shown in front of each opponent so they hide between hands.
  handInProgress: boolean;

  // My hole cards default to face-down each hand; tap to peek.
  myCardsRevealed: boolean;

  // My evaluated hand-rank label at showdown (e.g. "葫芦"); '' otherwise.
  myHandRank: string;
}

interface PageMethods {
  onAction(e: WechatMiniprogram.CustomEvent<PlayerAction>): void;
  onLeave(): void;
  onSettings(): void;
  onUnload(): void;
  onReady(): void;
  onResize(): void;
  onRebuy(): void;
  onToggleChatPicker(): void;
  onPickEmoji(e: WechatMiniprogram.TouchEvent): void;
  onChatBackdropTap(): void;
  onSettlementBackToLobby(): void;
  onToggleMyCards(): void;
  openSettlement(players: PlayerSettlement[]): void;
  shareRoom(): void;
  onShareAppMessage(): WechatMiniprogram.Page.ICustomShareContent;
  measureFelt(): void;
  getSeatOffsetPx(posKey: number): { dx: number; dy: number };
  getMeOffsetPx(): { dx: number; dy: number };
}

function toCardView(c: Card): MyCardView {
  return {
    suit: c.suit,
    rank: c.rank,
    symbol: SUIT_SYMBOL[c.suit],
    color: SUIT_COLOR[c.suit],
  };
}

let unsubAll: Array<() => void> = [];
let chipFlyIdSeq = 0;
let chatBubbleIdSeq = 0;
let winBubbleIdSeq = 0;
let countdownTimer: ReturnType<typeof setInterval> | null = null;
// Showdown hand-rank cache per uid for the current hand. The wire room-state
// only carries hole cards, not the evaluated rank, so we stash whatever the
// `showdown` game-event delivers and clear it at the start of the next hand.
let handRanksByUid: Record<string, string> = {};
let pageJoinPassword = '';
// True only when the user picked "离开房间" from the settings menu. The plain
// back navigation (system gesture / hardware back) keeps this false so the
// server's soft-leave grace can preserve the seat + chips for a quick re-entry.
let intentionalLeave = false;

// Felt-bounding-box cache so dealCards / chipFlies can use pixel transforms
// (which composite on GPU) instead of animating `top`/`left` percentages.
// Populated lazily on first deal via wx.createSelectorQuery; recomputed on
// orientation/resize via onResize.
interface FeltGeom {
  width: number;
  height: number;
  // Per-posKey (1..8) pixel offset from felt centre to the seat centre.
  seatOffsets: Record<number, { dx: number; dy: number }>;
  // Pixel offset from felt centre to the me-zone anchor (below felt).
  meOffsetY: number;
}
let feltGeom: FeltGeom | null = null;

function shallowCardsEqual(a: Card[] | undefined, b: Card[] | undefined): boolean {
  if (a === b) return true;
  if (!a || !b) return !a === !b;
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i].suit !== b[i].suit || a[i].rank !== b[i].rank) return false;
  }
  return true;
}

function formatRemaining(ms: number): string {
  if (ms <= 0) return '00:00';
  const totalSec = Math.floor(ms / 1000);
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  const mm = m < 10 ? `0${m}` : String(m);
  const ss = s < 10 ? `0${s}` : String(s);
  return `${mm}:${ss}`;
}

Page<PageData, PageMethods>({
  data: {
    table: null,
    opponents: [],
    myPlayer: null,
    myCardsView: [],
    opponentPositions: OPPONENT_POSITIONS_9,
    isMyTurn: false,
    callAmount: 0,
    canCheck: false,
    statusBarHeight: 20,
    connectionState: 'idle',
    banner: '',

    countdownText: '',
    endingHint: '',
    stageLabel: '',
    settlementVisible: false,
    settlementPlayers: [],

    isDealing: false,
    dealCards: [],
    newCommunityIndices: [],
    chipFlies: [],
    chatBubbles: [],
    winBubbles: [],
    chatPickerOpen: false,
    quickEmojis: QUICK_EMOJIS,
    handInProgress: false,
    myCardsRevealed: false,
    myHandRank: '',
  },

  onLoad(options) {
    const sysInfo = store.getSystemInfo();
    const statusBarHeight = sysInfo?.safeAreaTop ?? 20;
    this.setData({ statusBarHeight, connectionState: 'connecting' });

    const user = store.getUser();
    if (!user) {
      this.setData({ banner: '用户未初始化' });
      return;
    }
    const roomId = (options?.roomId as string) || '1234';
    const buyIn = Number(options?.buyIn ?? 1000);
    pageJoinPassword = String(options?.password ?? '');
    intentionalLeave = false;

    const offState = gameSocket.on<WireRoomState>('room-state', (msg) => {
      if (!msg.data) return;
      this.applyWireState(msg.data);
    });
    const offJoined = gameSocket.on<JoinedPayload>('joined', (msg) => {
      console.log('[table] joined', msg.data);
      this.setData({ banner: '' });
    });
    const offError = gameSocket.on<ErrorPayload>('error', (msg) => {
      const text = translateServerError(msg.data);
      console.warn('[table] server error', msg.data);
      wx.showToast({ title: text, icon: 'none', duration: 2000 });
    });
    const offEvent = gameSocket.on<GameEventPayload>('game-event', (msg) => {
      this.handleGameEvent(msg.data);
    });
    const offChat = gameSocket.on<ChatMessagePayload>('chat', (msg) => {
      if (!msg.data) return;
      this.spawnChatBubble(msg.data);
    });
    const offEnded = gameSocket.on<GameEndedPayload>('game-ended', (msg) => {
      if (!msg.data) return;
      this.openSettlement(msg.data.players);
    });
    const offAny = gameSocket.onAny((_m: ServerMessage) => {
      // diagnostic hook
    });
    unsubAll = [offState, offJoined, offError, offEvent, offChat, offEnded, offAny];

    // 1Hz countdown ticker (cheap; only runs while we're on this page).
    if (countdownTimer) clearInterval(countdownTimer);
    countdownTimer = setInterval(() => {
      const tbl = this.data.table;
      if (!tbl || tbl.endsAt <= 0) {
        if (this.data.countdownText !== '') this.setData({ countdownText: '' });
        return;
      }
      if (tbl.ended) {
        if (this.data.countdownText !== '已结束') this.setData({ countdownText: '已结束' });
        return;
      }
      const remaining = tbl.endsAt - Date.now();
      const text = formatRemaining(remaining);
      if (text !== this.data.countdownText) this.setData({ countdownText: text });
    }, 1000);

    this.setData({ banner: '获取登录凭据…' });
    ensureLoggedIn()
      .catch((err) => {
        // Soft-auth: log the failure but try connecting anyway (server may be
        // running without AUTH_REQUIRED so anonymous join still works).
        console.warn('[table] ensureLoggedIn failed (continuing anonymously)', err);
        return '';
      })
      .then((token) => {
        const fresh = store.getUser();
        const wsURL = token ? `${DEFAULT_WS_URL}?token=${encodeURIComponent(token)}` : DEFAULT_WS_URL;
        this.setData({ banner: `连接 ${wsURL.split('?')[0]} …` });
        const u = fresh ?? user;
        const sendJoin = () => {
          gameSocket.send('join', {
            roomId,
            userId: u.uid,
            nickname: u.nickname,
            avatar: u.avatar,
            buyIn,
            password: pageJoinPassword || undefined,
          });
        };
        return gameSocket
          .connect({
            url: wsURL,
            onClose: (info) => {
              this.setData({
                connectionState: 'disconnected',
                banner: `连接断开 code=${info.code}，重连中…`,
              });
            },
            onError: (err) => {
              const msg = (err as { errMsg?: string })?.errMsg || JSON.stringify(err);
              this.setData({ connectionState: 'disconnected', banner: `连接错误: ${msg}` });
            },
            reconnect: {
              maxAttempts: 6,
              baseDelayMs: 800,
              onReconnecting: (attempt, delayMs) => {
                this.setData({
                  banner: `第 ${attempt} 次重连，${Math.ceil(delayMs / 1000)}s 后重试…`,
                });
              },
              onReconnect: () => {
                this.setData({ connectionState: 'connected', banner: '重新加入中…' });
                sendJoin();
              },
              onGiveUp: () => {
                this.setData({ banner: '重连失败，请返回大厅再次进入' });
              },
            },
          })
          .then(() => {
            this.setData({ connectionState: 'connected', banner: '加入房间中…' });
            sendJoin();
          });
      })
      .catch((err) => {
        console.warn('[table] connect failed', err);
        const msg = (err as { errMsg?: string })?.errMsg || JSON.stringify(err);
        this.setData({ banner: `无法连接: ${msg}` });
      });
  },

  onUnload() {
    unsubAll.forEach((fn) => fn());
    unsubAll = [];
    if (countdownTimer) {
      clearInterval(countdownTimer);
      countdownTimer = null;
    }
    pageJoinPassword = '';
    feltGeom = null;
    // Only emit a hard leave when the user picked "离开房间" from the menu.
    // System back / swipe-back drops through to just closing the socket — the
    // server's disconnect grace preserves the seat for ~60s so a quick
    // re-entry keeps the same chip stack instead of resetting to fresh BuyIn.
    if (intentionalLeave && gameSocket.isConnected()) {
      gameSocket.send('leave', { roomId: this.data.table?.roomId });
    }
    intentionalLeave = false;
    gameSocket.close();
  },

  onReady() {
    // First-paint measurement of the felt's actual pixel rect. Used by
    // deal-card / chip-fly animations so target positions can be expressed in
    // pixel offsets (transform-only animation) instead of % top/left (which
    // would trigger layout on every frame).
    this.measureFelt();
  },

  onResize() {
    // Orientation/window-size change — re-measure so subsequent animations
    // use the new geometry.
    feltGeom = null;
    this.measureFelt();
  },

  applyWireState(wire: WireRoomState) {
    const user = store.getUser();
    if (!user) return;
    const prev = this.data.table;
    const table = wireToTable(wire, user.uid, prev);
    store.setRoomState(wire);

    const me = table.players.find((p) => p.isMe) ?? null;
    const mySeat = me?.seat ?? 0;
    const opponents: OpponentView[] = table.players
      .filter((p) => !p.isMe)
      .map((p) => ({
        ...p,
        posKey: ((p.seat - mySeat + table.maxSeats) % table.maxSeats),
        handRank: handRankLabel(handRanksByUid[p.uid]),
      }));
    const myHandRank = me ? handRankLabel(handRanksByUid[me.uid]) : '';
    const isMyTurn = !!me && table.activeSeat === me.seat;
    const callAmount = me ? Math.max(0, table.currentBet - me.betThisRound) : 0;
    const myCardsView = me ? me.holeCards.map(toCardView) : [];

    // Detect new community cards revealed and trigger flip-in for those indices.
    // Cards reveal one-by-one with a 220ms stagger inside the component, so the
    // cleanup needs to outlast (n-1)*220ms + 550ms animation.
    const prevRevealed = prev?.revealedCount ?? 0;
    const nowRevealed = table.revealedCount;
    let newIdx: number[] = [];
    if (nowRevealed > prevRevealed) {
      for (let i = prevRevealed; i < nowRevealed; i++) newIdx.push(i);
      const FLIP_STAGGER_MS = 220;
      const cleanupMs = (newIdx.length - 1) * FLIP_STAGGER_MS + 700;
      setTimeout(() => {
        this.setData({ newCommunityIndices: [] });
      }, cleanupMs);
      // Stagger the deal sound to match the visual flips
      newIdx.forEach((_, order) => {
        if (order === 0) sfx.play('deal');
        else setTimeout(() => sfx.play('deal'), order * FLIP_STAGGER_MS);
      });
    }

    // Detect a fresh hand starting (preflop with hole cards we didn't have before)
    const wasIdle = !prev || prev.stage === 'waiting' || prev.stage === 'hand-complete';
    const enteringPreflop = table.stage === 'preflop';

    // Clear lingering per-hand animations (win bubbles, chip flies) whenever
    // we leave a resolving stage. Without this the previous hand's "+N" float
    // can bleed into the next hand if the server auto-restarts inside its
    // 1.9s lifetime.
    const prevResolving = !!prev && (prev.stage === 'showdown' || prev.stage === 'hand-complete');
    const nowResolving = table.stage === 'showdown' || table.stage === 'hand-complete';
    if (prevResolving && !nowResolving) {
      if (this.data.winBubbles.length > 0 || this.data.chipFlies.length > 0) {
        this.setData({ winBubbles: [], chipFlies: [] });
      }
    }
    // Entering a resolving stage — auto-reveal my own hole cards so I don't
    // have to tap during showdown (and so the UI is consistent with opponents
    // who get their cards revealed on their seats).
    if (!prevResolving && nowResolving && !this.data.myCardsRevealed && myCardsView.length > 0) {
      this.setData({ myCardsRevealed: true });
    }

    if (wasIdle && enteringPreflop) {
      // New hand starting — discard any cached hand-rank labels from last hand.
      handRanksByUid = {};
      // Measure the felt once (no-op if already cached) so deal-card targets
      // can be expressed in pixel offsets from felt centre, letting the
      // keyframes animate `transform: translate(...)` on the compositor.
      if (!feltGeom) this.measureFelt();
      // Build sequential deal-card placements: first card to each opponent in
      // posKey order (clockwise from me), then to me; then a second pass for
      // the second card. Cards animate from felt centre with a per-card stagger.
      // If feltGeom isn't ready yet (first hand of session), fall back to a
      // best-effort estimate using window size; subsequent hands hit the cache.
      const orderedSeats: Array<{ isMe: boolean; dx: number; dy: number }> = [];
      const opponentsByPos = [...opponents].sort((a, b) => a.posKey - b.posKey);
      for (const op of opponentsByPos) {
        const off = this.getSeatOffsetPx(op.posKey);
        orderedSeats.push({ isMe: false, dx: off.dx, dy: off.dy });
      }
      // "Me" anchor: below the felt — use the felt-centre to me-zone delta.
      orderedSeats.push({
        isMe: true,
        dx: 0,
        dy: feltGeom ? feltGeom.meOffsetY : 240,
      });
      const dealCards: DealCardAnim[] = [];
      let id = 0;
      for (let round = 0; round < 2; round++) {
        orderedSeats.forEach((s, i) => {
          dealCards.push({
            id: id++,
            isMe: s.isMe,
            dx: s.dx,
            dy: s.dy,
            delay: (round * orderedSeats.length + i) * DEAL_STAGGER_MS,
          });
        });
      }
      const dealTotalMs = (dealCards.length - 1) * DEAL_STAGGER_MS + DEAL_CARD_ANIM_MS + 100;
      this.setData({ isDealing: true, dealCards, myCardsRevealed: false });
      setTimeout(() => this.setData({ isDealing: false, dealCards: [] }), dealTotalMs);
    }

    let endingHint = '';
    if (table.ended) {
      endingHint = '本局已结束';
    } else if (table.endPending) {
      endingHint = '时间到，本手打完后结算';
    }
    const isMyTurnFinal = !table.ended && isMyTurn;
    const HAND_ACTIVE_STAGES: GameStage[] = ['preflop', 'flop', 'turn', 'river', 'showdown'];
    const handInProgress = HAND_ACTIVE_STAGES.indexOf(table.stage) >= 0;
    const canCheck = callAmount === 0;

    // Build a path-syntax patch instead of one monolithic setData. On every
    // wire update most fields are unchanged — sending only the diff keeps the
    // JSBridge payload at tens of bytes instead of the kb-scale opponents
    // array. Opponents diff per index per field; the seat component re-renders
    // only when its actual data changes.
    const patch: Record<string, unknown> = {};
    const prevTbl = this.data.table;
    if (!prevTbl) {
      patch.table = table;
    } else {
      if (prevTbl.stage !== table.stage) patch['table.stage'] = table.stage;
      if (prevTbl.pot !== table.pot) patch['table.pot'] = table.pot;
      if (prevTbl.currentBet !== table.currentBet) patch['table.currentBet'] = table.currentBet;
      if (prevTbl.activeSeat !== table.activeSeat) patch['table.activeSeat'] = table.activeSeat;
      if (prevTbl.revealedCount !== table.revealedCount) patch['table.revealedCount'] = table.revealedCount;
      if (prevTbl.minRaise !== table.minRaise) patch['table.minRaise'] = table.minRaise;
      if (prevTbl.endsAt !== table.endsAt) patch['table.endsAt'] = table.endsAt;
      if (prevTbl.ended !== table.ended) patch['table.ended'] = table.ended;
      if (prevTbl.endPending !== table.endPending) patch['table.endPending'] = table.endPending;
      if (prevTbl.actionDeadline !== table.actionDeadline) patch['table.actionDeadline'] = table.actionDeadline;
      if (prevTbl.smallBlind !== table.smallBlind) patch['table.smallBlind'] = table.smallBlind;
      if (prevTbl.bigBlind !== table.bigBlind) patch['table.bigBlind'] = table.bigBlind;
      if (prevTbl.maxSeats !== table.maxSeats) patch['table.maxSeats'] = table.maxSeats;
      if (prevTbl.roomId !== table.roomId) patch['table.roomId'] = table.roomId;
      if (!shallowCardsEqual(prevTbl.communityCards, table.communityCards)) {
        patch['table.communityCards'] = table.communityCards;
      }
    }

    const prevOpps = this.data.opponents;
    if (prevOpps.length !== opponents.length) {
      patch.opponents = opponents;
    } else {
      for (let i = 0; i < opponents.length; i++) {
        const oldP = prevOpps[i];
        const newP = opponents[i];
        if (!oldP || oldP.uid !== newP.uid || oldP.posKey !== newP.posKey) {
          patch[`opponents[${i}]`] = newP;
          continue;
        }
        if (oldP.chips !== newP.chips) patch[`opponents[${i}].chips`] = newP.chips;
        if (oldP.betThisRound !== newP.betThisRound) patch[`opponents[${i}].betThisRound`] = newP.betThisRound;
        if (oldP.status !== newP.status) patch[`opponents[${i}].status`] = newP.status;
        if (oldP.isDealer !== newP.isDealer) patch[`opponents[${i}].isDealer`] = newP.isDealer;
        if (oldP.isSmallBlind !== newP.isSmallBlind) patch[`opponents[${i}].isSmallBlind`] = newP.isSmallBlind;
        if (oldP.isBigBlind !== newP.isBigBlind) patch[`opponents[${i}].isBigBlind`] = newP.isBigBlind;
        if (oldP.isUTG !== newP.isUTG) patch[`opponents[${i}].isUTG`] = newP.isUTG;
        if (oldP.handRank !== newP.handRank) patch[`opponents[${i}].handRank`] = newP.handRank;
        if (oldP.nickname !== newP.nickname) patch[`opponents[${i}].nickname`] = newP.nickname;
        if (oldP.avatar !== newP.avatar) patch[`opponents[${i}].avatar`] = newP.avatar;
        if (oldP.avatarIsUrl !== newP.avatarIsUrl) patch[`opponents[${i}].avatarIsUrl`] = newP.avatarIsUrl;
        if (oldP.rebuyCount !== newP.rebuyCount) patch[`opponents[${i}].rebuyCount`] = newP.rebuyCount;
        if (!shallowCardsEqual(oldP.holeCards, newP.holeCards)) {
          patch[`opponents[${i}].holeCards`] = newP.holeCards;
        }
      }
    }

    const prevMe = this.data.myPlayer;
    if (!me) {
      if (prevMe) patch.myPlayer = null;
    } else if (!prevMe) {
      patch.myPlayer = me;
    } else {
      if (prevMe.chips !== me.chips) patch['myPlayer.chips'] = me.chips;
      if (prevMe.betThisRound !== me.betThisRound) patch['myPlayer.betThisRound'] = me.betThisRound;
      if (prevMe.status !== me.status) patch['myPlayer.status'] = me.status;
      if (prevMe.isDealer !== me.isDealer) patch['myPlayer.isDealer'] = me.isDealer;
      if (prevMe.isSmallBlind !== me.isSmallBlind) patch['myPlayer.isSmallBlind'] = me.isSmallBlind;
      if (prevMe.isBigBlind !== me.isBigBlind) patch['myPlayer.isBigBlind'] = me.isBigBlind;
      if (prevMe.isUTG !== me.isUTG) patch['myPlayer.isUTG'] = me.isUTG;
      if (prevMe.nickname !== me.nickname) patch['myPlayer.nickname'] = me.nickname;
      if (prevMe.avatar !== me.avatar) patch['myPlayer.avatar'] = me.avatar;
      if (prevMe.avatarIsUrl !== me.avatarIsUrl) patch['myPlayer.avatarIsUrl'] = me.avatarIsUrl;
      if (prevMe.rebuyCount !== me.rebuyCount) patch['myPlayer.rebuyCount'] = me.rebuyCount;
      if (!shallowCardsEqual(prevMe.holeCards, me.holeCards)) {
        patch['myPlayer.holeCards'] = me.holeCards;
      }
    }

    const prevCardsView = this.data.myCardsView;
    if (prevCardsView.length !== myCardsView.length) {
      patch.myCardsView = myCardsView;
    } else {
      for (let i = 0; i < myCardsView.length; i++) {
        if (
          prevCardsView[i].suit !== myCardsView[i].suit ||
          prevCardsView[i].rank !== myCardsView[i].rank
        ) {
          patch.myCardsView = myCardsView;
          break;
        }
      }
    }

    if (this.data.isMyTurn !== isMyTurnFinal) patch.isMyTurn = isMyTurnFinal;
    if (this.data.callAmount !== callAmount) patch.callAmount = callAmount;
    if (this.data.canCheck !== canCheck) patch.canCheck = canCheck;
    if (this.data.banner !== '') patch.banner = '';
    if (this.data.endingHint !== endingHint) patch.endingHint = endingHint;
    if (this.data.handInProgress !== handInProgress) patch.handInProgress = handInProgress;
    if (this.data.myHandRank !== myHandRank) patch.myHandRank = myHandRank;
    const stageLabel = STAGE_LABEL[table.stage] ?? table.stage;
    if (this.data.stageLabel !== stageLabel) patch.stageLabel = stageLabel;
    if (newIdx.length) patch.newCommunityIndices = newIdx;

    if (Object.keys(patch).length > 0) this.setData(patch);
  },

  openSettlement(players: PlayerSettlement[]) {
    const enriched = players.map((p) => ({
      ...p,
      avatarIsUrl: !!p.avatar && (p.avatar.indexOf('/') >= 0 || p.avatar.indexOf(':') >= 0),
    }));
    this.setData({
      settlementVisible: true,
      settlementPlayers: enriched,
    });
  },

  onSettlementBackToLobby() {
    intentionalLeave = true;
    wx.navigateBack({ delta: 1, fail: () => wx.switchTab({ url: '/pages/lobby/lobby' }) });
  },

  handleGameEvent(payload: GameEventPayload | undefined) {
    if (!payload) return;
    console.log('[table] event', payload.type, payload.data);

    switch (payload.type) {
      case 'action':
      case 'blind-posted': {
        // Spawn a chip flying from the actor's seat to the pot
        const seat = payload.data?.seat as number | undefined;
        const amount = (payload.data?.amount ?? 0) as number;
        const actType = String(payload.data?.type || '');
        if (typeof seat === 'number' && amount > 0) {
          this.spawnChipFly(seat, amount);
          sfx.play('chip');
        } else if (actType === 'Fold') {
          sfx.play('fold');
        }
        break;
      }
      case 'hole-dealt': {
        sfx.play('deal');
        break;
      }
      case 'community-dealt': {
        // Deal sounds for community cards are played staggered from
        // applyWireState so they line up with the per-card flip animation.
        break;
      }
      case 'showdown': {
        const shares = (payload.data?.shares as Array<{ playerId: string; amount: number }>) || [];
        const netByUid = (payload.data?.net as Record<string, number>) || {};
        const hands = (payload.data?.hands as Array<{ playerId: string; rank: string }>) || [];
        // Cache per-uid hand rank so the UI can label each showdown player.
        // This is cleared at the start of the next hand.
        for (const h of hands) {
          if (h && h.playerId && h.rank) handRanksByUid[h.playerId] = h.rank;
        }
        // Re-derive my own rank label immediately so the me-zone updates
        // without waiting for the next room-state.
        const me = this.data.myPlayer;
        if (me && handRanksByUid[me.uid]) {
          this.setData({ myHandRank: handRankLabel(handRanksByUid[me.uid]) });
        }
        this.flyPotToWinners(shares, netByUid);
        sfx.play('win');
        break;
      }
      case 'hand-complete': {
        if (payload.data?.uncontested) {
          const winnerSeat = payload.data?.winner as number | undefined;
          const amount = (payload.data?.amount as number) ?? 0;
          const net = (payload.data?.net as number) ?? amount;
          if (typeof winnerSeat === 'number') {
            this.flyPotToSeat(winnerSeat, amount);
            this.spawnWinBubble(winnerSeat, net);
          }
        }
        break;
      }
    }
  },

  flyPotToWinners(
    shares: Array<{ playerId: string; amount: number }>,
    netByUid: Record<string, number>,
  ) {
    const tbl = this.data.table;
    if (!tbl) return;
    // Aggregate gross winnings per uid (drives chip-fly + on-chip number).
    const totalByUid: Record<string, number> = {};
    for (const s of shares) {
      totalByUid[s.playerId] = (totalByUid[s.playerId] || 0) + s.amount;
    }
    for (const uid of Object.keys(totalByUid)) {
      const amount = totalByUid[uid];
      const player = tbl.players.find((p) => p.uid === uid);
      if (!player) continue;
      this.flyPotToSeat(player.seat, amount);
      // +N float uses net profit (gross - what they put in this hand).
      const net = netByUid[uid] ?? amount;
      this.spawnWinBubble(player.seat, net);
    }
  },

  // Measure the felt's actual pixel size + cache pixel offsets from the felt
  // centre to each opponent seat anchor. Run once on page-ready (fire-and-
  // forget); used by deal-card/chip-fly animations so their keyframes can stay
  // on the compositor instead of animating top/left.
  measureFelt() {
    const q = wx.createSelectorQuery().in(this);
    q.select('.felt').boundingClientRect();
    q.exec((res: WechatMiniprogram.NodeInfo[]) => {
      const rect = res && res[0];
      if (!rect || !rect.width || !rect.height) return;
      const seatOffsets: Record<number, { dx: number; dy: number }> = {};
      Object.entries(OPPONENT_POSITIONS_9).forEach(([k, pos]) => {
        const tx = (parseFloat(pos.left) / 100) * rect.width;
        const ty = (parseFloat(pos.top) / 100) * rect.height;
        seatOffsets[Number(k)] = {
          dx: tx - rect.width / 2,
          dy: ty - rect.height / 2,
        };
      });
      // "me" zone sits below the felt; rough px estimate from the legacy 110%.
      const meOffsetY = rect.height * 0.6;
      feltGeom = { width: rect.width, height: rect.height, seatOffsets, meOffsetY };
    });
  },

  // Returns pixel offset from felt centre for a given posKey. Falls back to a
  // window-size estimate when the felt hasn't been measured yet (e.g. very
  // first hand of the session before onReady's measurement completes).
  getSeatOffsetPx(posKey: number): { dx: number; dy: number } {
    if (feltGeom) {
      return feltGeom.seatOffsets[posKey] ?? { dx: 0, dy: 0 };
    }
    const sys = store.getSystemInfo();
    const ww = sys?.windowWidth ?? 375;
    const wh = sys?.windowHeight ?? 667;
    const fw = ww * 0.92;
    const fh = (wh - 160) * 0.55; // very rough: page minus topbar/me-zone/action
    const pos = OPPONENT_POSITIONS_9[posKey];
    if (!pos) return { dx: 0, dy: 0 };
    return {
      dx: (parseFloat(pos.left) / 100) * fw - fw / 2,
      dy: (parseFloat(pos.top) / 100) * fh - fh / 2,
    };
  },

  // Pixel offset from felt centre to the me-zone anchor (below the felt).
  getMeOffsetPx(): { dx: number; dy: number } {
    if (feltGeom) return { dx: 0, dy: feltGeom.meOffsetY };
    const sys = store.getSystemInfo();
    const wh = sys?.windowHeight ?? 667;
    return { dx: 0, dy: (wh - 160) * 0.33 };
  },

  flyPotToSeat(seatNumber: number, amount: number) {
    const me = this.data.myPlayer;
    if (!me || !this.data.table) return;
    const isMe = seatNumber === me.seat;
    const off = isMe
      ? this.getMeOffsetPx()
      : this.getSeatOffsetPx((seatNumber - me.seat + this.data.table.maxSeats) % this.data.table.maxSeats);
    const id = ++chipFlyIdSeq;
    const fly: ChipFly = {
      id,
      fromX: 0,
      fromY: 0,
      toX: off.dx,
      toY: off.dy,
      amount,
      hidden: false,
    };
    const idx = this.data.chipFlies.length;
    this.setData({ [`chipFlies[${idx}]`]: fly });
    setTimeout(() => {
      const cur = this.data.chipFlies;
      const at = cur.findIndex((c) => c.id === id);
      if (at >= 0 && !cur[at].hidden) {
        this.setData({ [`chipFlies[${at}].hidden`]: true });
      }
    }, 750);
  },

  spawnChipFly(seatNumber: number, amount: number) {
    const me = this.data.myPlayer;
    if (!me || !this.data.table) return;
    const isMe = seatNumber === me.seat;
    const off = isMe
      ? this.getMeOffsetPx()
      : this.getSeatOffsetPx((seatNumber - me.seat + this.data.table.maxSeats) % this.data.table.maxSeats);
    const id = ++chipFlyIdSeq;
    const fly: ChipFly = {
      id,
      fromX: off.dx,
      fromY: off.dy,
      toX: 0,
      toY: 0,
      amount,
      hidden: false,
    };
    const idx = this.data.chipFlies.length;
    this.setData({ [`chipFlies[${idx}]`]: fly });
    setTimeout(() => {
      const cur = this.data.chipFlies;
      const at = cur.findIndex((c) => c.id === id);
      if (at >= 0 && !cur[at].hidden) {
        this.setData({ [`chipFlies[${at}].hidden`]: true });
      }
    }, 700);
  },

  spawnWinBubble(seatNumber: number, amount: number) {
    const me = this.data.myPlayer;
    const tbl = this.data.table;
    if (!tbl || amount <= 0) return;
    // Anchor at the winner's avatar position (same coords as their seat).
    let top = '95%';
    let left = '50%';
    if (me && seatNumber === me.seat) {
      // Me-zone sits below the felt; place the bubble just above the me-avatar.
      top = '100%';
      left = '50%';
    } else if (me) {
      const posKey = (seatNumber - me.seat + tbl.maxSeats) % tbl.maxSeats;
      const pos = OPPONENT_POSITIONS_9[posKey];
      if (pos) {
        top = pos.top;
        left = pos.left;
      }
    }
    const id = ++winBubbleIdSeq;
    const bubble: WinBubble = { id, seat: seatNumber, top, left, amount, hidden: false };
    const idx = this.data.winBubbles.length;
    this.setData({ [`winBubbles[${idx}]`]: bubble });
    setTimeout(() => {
      const cur = this.data.winBubbles;
      const at = cur.findIndex((b) => b.id === id);
      if (at >= 0 && !cur[at].hidden) {
        this.setData({ [`winBubbles[${at}].hidden`]: true });
      }
    }, 1900);
  },

  spawnChatBubble(payload: ChatMessagePayload) {
    const me = this.data.myPlayer;
    const tbl = this.data.table;
    if (!tbl) return;
    let top = '95%';
    let left = '50%';
    if (me && payload.seat === me.seat) {
      // From self — anchor at me-zone top
      top = '100%';
      left = '50%';
    } else if (me) {
      const posKey = (payload.seat - me.seat + tbl.maxSeats) % tbl.maxSeats;
      const pos = OPPONENT_POSITIONS_9[posKey];
      if (pos) {
        top = pos.top;
        left = pos.left;
      }
    }
    const id = ++chatBubbleIdSeq;
    const bubble: ChatBubble = { id, seat: payload.seat, top, left, emoji: payload.emoji, hidden: false };
    const idx = this.data.chatBubbles.length;
    this.setData({ [`chatBubbles[${idx}]`]: bubble });
    setTimeout(() => {
      const cur = this.data.chatBubbles;
      const at = cur.findIndex((b) => b.id === id);
      if (at >= 0 && !cur[at].hidden) {
        this.setData({ [`chatBubbles[${at}].hidden`]: true });
      }
    }, 2200);
  },

  onToggleChatPicker() {
    this.setData({ chatPickerOpen: !this.data.chatPickerOpen });
  },

  onChatBackdropTap() {
    this.setData({ chatPickerOpen: false });
  },

  onToggleMyCards() {
    if (this.data.myCardsView.length === 0) return;
    this.setData({ myCardsRevealed: !this.data.myCardsRevealed });
  },

  onPickEmoji(e) {
    const emoji = String(e.currentTarget.dataset.emoji || '');
    this.setData({ chatPickerOpen: false });
    const roomId = this.data.table?.roomId;
    if (!emoji || !roomId) return;
    gameSocket.send('chat', { roomId, emoji });
  },

  onAction(e) {
    const act = e.detail;
    const roomId = this.data.table?.roomId;
    if (!roomId) return;
    gameSocket.send('action', { roomId, type: act.type, amount: act.amount });
  },

  onRebuy() {
    const roomId = this.data.table?.roomId;
    if (!roomId) return;
    const amount = (this.data.table?.bigBlind ?? 100) * 10; // default 10 BB
    wx.showModal({
      title: '加买',
      content: `加买 ${amount} 筹码继续游戏？`,
      confirmText: '加买',
      cancelText: '取消',
      success: (res) => {
        if (res.confirm) {
          gameSocket.send('rebuy', { roomId, amount });
        }
      },
    });
  },

  onLeave() {
    intentionalLeave = true;
    wx.navigateBack({ delta: 1 });
  },

  onSettings() {
    const muted = sfx.isMuted();
    const items = [
      muted ? '🔊 打开音效' : '🔇 关闭音效',
      '🔗 分享房间',
      '🚪 离开房间',
    ];
    wx.showActionSheet({
      itemList: items,
      success: (res) => {
        switch (res.tapIndex) {
          case 0:
            sfx.setMuted(!muted);
            wx.showToast({ title: muted ? '音效已开' : '音效已关', icon: 'none' });
            break;
          case 1:
            this.shareRoom();
            break;
          case 2:
            wx.showModal({
              title: '离开房间',
              content: '本手牌仍在进行中将自动弃牌。确定离开？',
              confirmText: '离开',
              cancelText: '取消',
              success: (m) => {
                if (m.confirm) this.onLeave();
              },
            });
            break;
        }
      },
    });
  },

  shareRoom() {
    const roomId = this.data.table?.roomId;
    if (!roomId) return;
    // The share button (•••) on the nav bar uses onShareAppMessage. From an
    // action-sheet we can only suggest copying the room id.
    wx.setClipboardData({
      data: `房间号 #${roomId}`,
      success: () => wx.showToast({ title: '房间号已复制，点 ⋯ 转发好友', icon: 'none', duration: 2200 }),
    });
  },

  onShareAppMessage() {
    const roomId = this.data.table?.roomId;
    const buyIn = this.data.myPlayer?.chips ?? 1000;
    return {
      title: roomId ? `德州扑克 · 房间 #${roomId}，来一把？` : '德州扑克娱乐',
      path: roomId ? `/pages/table/table?roomId=${roomId}&buyIn=${buyIn}` : '/pages/index/index',
    };
  },
});
