import { store } from '../../utils/store';
import { gameSocket, DEFAULT_WS_URL } from '../../utils/socket';
import { wireToTable } from '../../utils/wire-adapter';
import { SUIT_SYMBOL, SUIT_COLOR } from '../../utils/cards';
import { ensureLoggedIn } from '../../utils/auth';
import * as sfx from '../../utils/sfx';
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
  1: { top: '78%', left: '88%' },
  2: { top: '50%', left: '96%' },
  3: { top: '22%', left: '88%' },
  4: { top: '8%',  left: '66%' },
  5: { top: '6%',  left: '50%' },
  6: { top: '8%',  left: '34%' },
  7: { top: '22%', left: '12%' },
  8: { top: '50%', left: '4%' },
};

interface MyCardView {
  suit: Suit | '';
  rank: string;
  symbol: string;
  color: 'red' | 'black';
}

interface OpponentView extends Player {
  posKey: number; // 1..8, relative to my seat clockwise (9-seat table)
}

interface ShowdownContestant {
  userId: string;
  nickname: string;
  avatar: string;
  avatarIsUrl: boolean;
  rankSlug: string;
  holeCards: Card[];
  amountWon: number;
  isWinner: boolean;
}

interface ChipFly {
  id: number;
  fromTop: string;
  fromLeft: string;
  toTop: string;
  toLeft: string;
  amount: number;
}

interface ChatBubble {
  id: number;
  seat: number;
  top: string;
  left: string;
  emoji: string;
}

interface DealCardAnim {
  id: number;
  isMe: boolean;
  toTop: string;   // unused when isMe, but provided for uniform style binding
  toLeft: string;
  delay: number;   // ms, drives sequential fan-out
}

const DEAL_STAGGER_MS = 120;
const DEAL_CARD_ANIM_MS = 450;

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

  // Showdown panel
  showdownVisible: boolean;
  showdownContestants: ShowdownContestant[];
  showdownCommunity: Card[];
  uncontestedWinnerName: string;
  uncontestedAmount: number;

  // Game-duration countdown (only shown when endsAt > 0)
  countdownText: string;
  endingHint: string;

  // End-of-game settlement panel
  settlementVisible: boolean;
  settlementPlayers: PlayerSettlement[];

  // Animations
  isDealing: boolean;            // hole card deal animation playing
  dealCards: DealCardAnim[];     // pre-computed sequential deal placements
  newCommunityIndices: number[]; // indices of community cards just revealed (for flip-in)
  chipFlies: ChipFly[];          // active chip-to-pot animations
  chatBubbles: ChatBubble[];     // active floating chat bubbles
  chatPickerOpen: boolean;
  quickEmojis: string[];

  // True while a hand is being dealt/played — gates the face-down hole-card
  // backs shown in front of each opponent so they hide between hands.
  handInProgress: boolean;

  // My hole cards default to face-down each hand; tap to peek.
  myCardsRevealed: boolean;
}

interface PageMethods {
  onAction(e: WechatMiniprogram.CustomEvent<PlayerAction>): void;
  onLeave(): void;
  onSettings(): void;
  onUnload(): void;
  onShowdownDismiss(): void;
  onRebuy(): void;
  onToggleChatPicker(): void;
  onPickEmoji(e: WechatMiniprogram.TouchEvent): void;
  onChatBackdropTap(): void;
  onSettlementBackToLobby(): void;
  onToggleMyCards(): void;
  openSettlement(players: PlayerSettlement[]): void;
  shareRoom(): void;
  onShareAppMessage(): WechatMiniprogram.Page.ICustomShareContent;
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
let countdownTimer: ReturnType<typeof setInterval> | null = null;
let pageJoinPassword = '';
// True only when the user picked "离开房间" from the settings menu. The plain
// back navigation (system gesture / hardware back) keeps this false so the
// server's soft-leave grace can preserve the seat + chips for a quick re-entry.
let intentionalLeave = false;

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

    showdownVisible: false,
    showdownContestants: [],
    showdownCommunity: [],
    uncontestedWinnerName: '',
    uncontestedAmount: 0,

    countdownText: '',
    endingHint: '',
    settlementVisible: false,
    settlementPlayers: [],

    isDealing: false,
    dealCards: [],
    newCommunityIndices: [],
    chipFlies: [],
    chatBubbles: [],
    chatPickerOpen: false,
    quickEmojis: QUICK_EMOJIS,
    handInProgress: false,
    myCardsRevealed: false,
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
      const text = msg.data?.message || msg.data?.code || 'unknown error';
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
      }));
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
    if (wasIdle && enteringPreflop) {
      // Build sequential deal-card placements: first card to each opponent in
      // posKey order (clockwise from me), then to me; then a second pass for
      // the second card. Cards animate from felt centre with a per-card stagger.
      const orderedSeats: Array<{ isMe: boolean; top: string; left: string }> = [];
      const opponentsByPos = [...opponents].sort((a, b) => a.posKey - b.posKey);
      for (const op of opponentsByPos) {
        const pos = OPPONENT_POSITIONS_9[op.posKey];
        if (pos) orderedSeats.push({ isMe: false, top: pos.top, left: pos.left });
      }
      orderedSeats.push({ isMe: true, top: '110%', left: '50%' });
      const dealCards: DealCardAnim[] = [];
      let id = 0;
      for (let round = 0; round < 2; round++) {
        orderedSeats.forEach((s, i) => {
          dealCards.push({
            id: id++,
            isMe: s.isMe,
            toTop: s.top,
            toLeft: s.left,
            delay: (round * orderedSeats.length + i) * DEAL_STAGGER_MS,
          });
        });
      }
      const dealTotalMs = (dealCards.length - 1) * DEAL_STAGGER_MS + DEAL_CARD_ANIM_MS + 100;
      this.setData({ isDealing: true, dealCards, myCardsRevealed: false });
      setTimeout(() => this.setData({ isDealing: false, dealCards: [] }), dealTotalMs);
      // Dismiss any lingering showdown panel from the previous hand
      if (this.data.showdownVisible) {
        this.setData({
          showdownVisible: false,
          showdownContestants: [],
          showdownCommunity: [],
          uncontestedWinnerName: '',
          uncontestedAmount: 0,
        });
      }
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

    this.setData({
      table,
      myPlayer: me,
      opponents,
      myCardsView,
      isMyTurn: isMyTurnFinal,
      callAmount,
      canCheck: callAmount === 0,
      banner: '',
      endingHint,
      newCommunityIndices: newIdx.length ? newIdx : this.data.newCommunityIndices,
      handInProgress,
    });
  },

  openSettlement(players: PlayerSettlement[]) {
    const enriched = players.map((p) => ({
      ...p,
      avatarIsUrl: !!p.avatar && (p.avatar.indexOf('/') >= 0 || p.avatar.indexOf(':') >= 0),
    }));
    this.setData({
      settlementVisible: true,
      settlementPlayers: enriched,
      // close the per-hand showdown panel so it doesn't double-stack
      showdownVisible: false,
      showdownContestants: [],
      showdownCommunity: [],
      uncontestedWinnerName: '',
      uncontestedAmount: 0,
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
        const community = (payload.data?.community as Card[]) || [];
        const hands = (payload.data?.hands as Array<{ playerId: string; rank: string; holeCards: Card[] }>) || [];
        const shares = (payload.data?.shares as Array<{ playerId: string; amount: number }>) || [];
        this.flyPotToWinners(shares);
        this.openShowdown(community, hands, shares);
        sfx.play('win');
        break;
      }
      case 'hand-complete': {
        if (payload.data?.uncontested) {
          // winnerSeat → look up nickname
          const winnerSeat = payload.data?.winner as number | undefined;
          const amount = (payload.data?.amount as number) ?? 0;
          const tbl = this.data.table;
          const winner = tbl?.players.find((p) => p.seat === winnerSeat);
          if (typeof winnerSeat === 'number') {
            this.flyPotToSeat(winnerSeat, amount);
          }
          this.setData({
            showdownVisible: true,
            uncontestedWinnerName: winner?.nickname || '玩家',
            uncontestedAmount: amount,
            showdownContestants: [],
          });
        }
        break;
      }
    }
  },

  openShowdown(
    community: Card[],
    hands: Array<{ playerId: string; rank: string; holeCards: Card[] }>,
    shares: Array<{ playerId: string; amount: number }>,
  ) {
    const tbl = this.data.table;
    if (!tbl) return;
    // Aggregate share amounts per player
    const wonByUid: Record<string, number> = {};
    for (const s of shares) {
      wonByUid[s.playerId] = (wonByUid[s.playerId] || 0) + s.amount;
    }
    const contestants: ShowdownContestant[] = hands.map((h) => {
      const player = tbl.players.find((p) => p.uid === h.playerId);
      const won = wonByUid[h.playerId] || 0;
      const av = player?.avatar || '🃏';
      return {
        userId: h.playerId,
        nickname: player?.nickname || h.playerId,
        avatar: av,
        avatarIsUrl: !!av && (av.indexOf('/') >= 0 || av.indexOf(':') >= 0),
        rankSlug: h.rank,
        holeCards: h.holeCards,
        amountWon: won,
        isWinner: won > 0,
      };
    });
    // Sort: winners first, then by hand rank descending name (alpha is fine)
    contestants.sort((a, b) => Number(b.isWinner) - Number(a.isWinner));
    this.setData({
      showdownVisible: true,
      showdownContestants: contestants,
      showdownCommunity: community,
      uncontestedWinnerName: '',
      uncontestedAmount: 0,
    });
  },

  flyPotToWinners(shares: Array<{ playerId: string; amount: number }>) {
    const tbl = this.data.table;
    if (!tbl) return;
    // Aggregate amounts per uid
    const totalByUid: Record<string, number> = {};
    for (const s of shares) {
      totalByUid[s.playerId] = (totalByUid[s.playerId] || 0) + s.amount;
    }
    for (const uid of Object.keys(totalByUid)) {
      const amount = totalByUid[uid];
      const player = tbl.players.find((p) => p.uid === uid);
      if (!player) continue;
      this.flyPotToSeat(player.seat, amount);
    }
  },

  flyPotToSeat(seatNumber: number, amount: number) {
    const me = this.data.myPlayer;
    if (!me || !this.data.table) return;
    const isMe = seatNumber === me.seat;
    let toTop = '95%';
    let toLeft = '50%';
    if (!isMe) {
      const posKey = (seatNumber - me.seat + this.data.table.maxSeats) % this.data.table.maxSeats;
      const pos = OPPONENT_POSITIONS_9[posKey];
      if (pos) {
        toTop = pos.top;
        toLeft = pos.left;
      }
    }
    const id = ++chipFlyIdSeq;
    const fly: ChipFly = {
      id,
      fromTop: '50%',
      fromLeft: '50%',
      toTop,
      toLeft,
      amount,
    };
    this.setData({ chipFlies: [...this.data.chipFlies, fly] });
    setTimeout(() => {
      this.setData({
        chipFlies: this.data.chipFlies.filter((c) => c.id !== id),
      });
    }, 750);
  },

  spawnChipFly(seatNumber: number, amount: number) {
    const me = this.data.myPlayer;
    if (!me || !this.data.table) return;
    const isMe = seatNumber === me.seat;
    let fromTop = '50%';
    let fromLeft = '50%';
    if (isMe) {
      // me-zone is below the felt; chips originate from somewhere near the bottom
      fromTop = '95%';
      fromLeft = '50%';
    } else {
      const posKey = (seatNumber - me.seat + this.data.table.maxSeats) % this.data.table.maxSeats;
      const pos = OPPONENT_POSITIONS_9[posKey];
      if (pos) {
        fromTop = pos.top;
        fromLeft = pos.left;
      }
    }
    const id = ++chipFlyIdSeq;
    const fly: ChipFly = {
      id,
      fromTop,
      fromLeft,
      toTop: '50%',
      toLeft: '50%',
      amount,
    };
    this.setData({ chipFlies: [...this.data.chipFlies, fly] });
    // Cleanup after animation
    setTimeout(() => {
      this.setData({
        chipFlies: this.data.chipFlies.filter((c) => c.id !== id),
      });
    }, 700);
  },

  onShowdownDismiss() {
    this.setData({ showdownVisible: false });
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
    const bubble: ChatBubble = { id, seat: payload.seat, top, left, emoji: payload.emoji };
    this.setData({ chatBubbles: [...this.data.chatBubbles, bubble] });
    setTimeout(() => {
      this.setData({
        chatBubbles: this.data.chatBubbles.filter((b) => b.id !== id),
      });
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
