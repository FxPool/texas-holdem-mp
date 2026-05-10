import type {
  Player,
  PlayerStatus,
  TableState,
  WirePlayer,
  WireRoomState,
} from '../types/game';

const ACTION_TIMEOUT_MS = 30_000;

/**
 * Convert a server room-state into the UI's TableState. Tracks an action
 * deadline locally: callers pass `prevState` so we can preserve the deadline
 * while activeSeat is unchanged, and reset to now+30s when it changes.
 */
export function wireToTable(
  wire: WireRoomState,
  myUid: string,
  prev: TableState | null,
): TableState {
  const players: Player[] = wire.players.map((wp) => toUiPlayer(wp, myUid, wire.activeSeat));
  // sort by seat for stable rendering
  players.sort((a, b) => a.seat - b.seat);

  let actionDeadline = prev?.actionDeadline ?? Date.now() + ACTION_TIMEOUT_MS;
  if (!prev || prev.activeSeat !== wire.activeSeat) {
    actionDeadline = Date.now() + ACTION_TIMEOUT_MS;
  }

  return {
    roomId: wire.roomId,
    maxSeats: 6,
    players,
    communityCards: wire.community || [],
    revealedCount: wire.revealedCount,
    pot: wire.pot,
    currentBet: wire.currentBet,
    minRaise: wire.minRaise,
    stage: wire.stage,
    activeSeat: wire.activeSeat,
    actionDeadline,
    smallBlind: wire.smallBlind,
    bigBlind: wire.bigBlind,
    endsAt: wire.endsAt ?? 0,
    endPending: wire.endPending ?? false,
    ended: wire.ended ?? false,
    hasPassword: wire.hasPassword ?? false,
  };
}

function toUiPlayer(wp: WirePlayer, myUid: string, activeSeat: number): Player {
  const isMe = wp.userId === myUid;
  return {
    seat: wp.seat,
    uid: wp.userId,
    nickname: wp.nickname,
    avatar: wp.avatar,
    chips: wp.chips,
    betThisRound: wp.betThisRound,
    status: deriveStatus(wp, activeSeat),
    holeCards: wp.holeCards ?? [],
    isMe,
    isDealer: wp.isDealer,
    isSmallBlind: wp.isSmallBlind,
    isBigBlind: wp.isBigBlind,
  };
}

function deriveStatus(wp: WirePlayer, activeSeat: number): PlayerStatus {
  switch (wp.state) {
    case 'folded':
      return 'folded';
    case 'all-in':
      return 'all-in';
    case 'sit-out':
      return 'sit-out';
    case 'waiting':
      return 'waiting';
    case 'active':
      if (wp.seat === activeSeat) return 'thinking';
      return wp.hasActed ? 'acted' : 'waiting';
  }
  return 'waiting';
}
