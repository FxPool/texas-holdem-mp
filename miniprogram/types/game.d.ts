// 德州扑克游戏类型定义

export type Suit = 'spade' | 'heart' | 'club' | 'diamond';

export type Rank = '2' | '3' | '4' | '5' | '6' | '7' | '8' | '9' | '10' | 'J' | 'Q' | 'K' | 'A';

export interface Card {
  suit: Suit;
  rank: Rank;
}

// UI 状态：由 wire 状态 + 当前是否轮到行动 + hasActed 派生
export type PlayerStatus =
  | 'waiting'    // 还没行动过
  | 'thinking'   // 当前行动中
  | 'acted'      // 本轮已行动
  | 'folded'
  | 'all-in'
  | 'sit-out';

export interface Player {
  seat: number;
  uid: string;
  nickname: string;
  avatar: string;
  chips: number;
  betThisRound: number;
  status: PlayerStatus;
  holeCards: Card[];
  isMe: boolean;
  isDealer: boolean;
  isSmallBlind: boolean;
  isBigBlind: boolean;
}

export type GameStage = 'preflop' | 'flop' | 'turn' | 'river' | 'showdown' | 'hand-complete' | 'waiting';

export interface TableState {
  roomId: string;
  maxSeats: number;
  players: Player[];
  communityCards: Card[];
  revealedCount: number;
  pot: number;
  currentBet: number;
  minRaise: number;
  stage: GameStage;
  activeSeat: number;
  actionDeadline: number;   // 客户端本地估算的行动截止时间戳 ms
  smallBlind: number;
  bigBlind: number;
}

export type ActionType = 'fold' | 'check' | 'call' | 'raise' | 'all-in';

export interface PlayerAction {
  type: ActionType;
  amount?: number;
}

// =========================
// Wire protocol (server <-> client) — must match server/internal/ws/protocol.go
// =========================

export type WirePlayerState = 'sit-out' | 'active' | 'folded' | 'all-in' | 'waiting';

export interface WirePlayer {
  userId: string;
  seat: number;
  nickname: string;
  avatar: string;
  chips: number;
  betThisRound: number;
  state: WirePlayerState;
  holeCards?: Card[];
  hasActed: boolean;
  isDealer: boolean;
  isSmallBlind: boolean;
  isBigBlind: boolean;
}

export interface WireRoomState {
  roomId: string;
  stage: GameStage;
  pot: number;
  currentBet: number;
  minRaise: number;
  activeSeat: number;
  dealerSeat: number;
  smallBlind: number;
  bigBlind: number;
  community: Card[];
  revealedCount: number;
  players: WirePlayer[];
  viewerSeat: number;
}

export type ServerMsgType =
  | 'joined'
  | 'error'
  | 'room-state'
  | 'game-event'
  | 'player-joined'
  | 'player-left'
  | 'chat'
  | 'pong';

export interface ServerMessage<T = unknown> {
  type: ServerMsgType;
  data?: T;
}

export interface JoinedPayload {
  roomId: string;
  seat: number;
  userId: string;
}

export interface ErrorPayload {
  code: string;
  message: string;
}

export interface GameEventPayload {
  type: string;
  data?: Record<string, unknown>;
}

export type ClientMsgType = 'join' | 'leave' | 'action' | 'rebuy' | 'chat' | 'ping';

export interface ClientMessage<T = unknown> {
  type: ClientMsgType;
  data?: T;
}

export interface JoinPayload {
  roomId: string;
  userId: string;
  nickname: string;
  avatar: string;
  buyIn: number;
}

export interface ActionPayload {
  roomId: string;
  type: ActionType;
  amount?: number;
}

export interface RebuyPayload {
  roomId: string;
  amount: number;
}

export interface ChatPayload {
  roomId: string;
  emoji: string;
}

export interface ChatMessagePayload {
  userId: string;
  seat: number;
  emoji: string;
  ts: number;
}
