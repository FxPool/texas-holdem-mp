// Translates server-side error payloads (code + message) into user-friendly
// Chinese strings for toast display. The server emits English error text
// (Go's fmt.Errorf produces raw strings like "player 7 state Folded cannot
// act"), and we'd rather not change the wire format, so the translation
// happens at the UI boundary.

const CODE_LABEL: Record<string, string> = {
  'bad-payload': '请求参数错误',
  'bad-action': '动作参数错误',
  'join-failed': '加入房间失败',
  'rebuy-failed': '加买失败',
  'action-failed': '操作失败',
  'not-joined': '尚未加入房间',
  'no-room': '房间不存在',
  'rate-limited': '操作太频繁,请稍候',
  'password-required': '需要房间密码',
  'chat-blocked': '聊天内容被拦截',
};

const STATE_LABEL: Record<string, string> = {
  Folded: '弃牌',
  AllIn: '全下',
  'All-In': '全下',
  SitOut: '离座',
  Waiting: '等待中',
  Acted: '已行动',
  Active: '行动中',
};

const STAGE_LABEL: Record<string, string> = {
  Waiting: '等待中',
  Preflop: '翻前',
  Flop: '翻牌',
  Turn: '转牌',
  River: '河牌',
  Showdown: '摊牌',
  HandComplete: '本手结束',
  preflop: '翻前',
  flop: '翻牌',
  turn: '转牌',
  river: '河牌',
  showdown: '摊牌',
  waiting: '等待中',
  'hand-complete': '本手结束',
};

function localizeState(s: string): string {
  return STATE_LABEL[s] ?? s;
}

function localizeStage(s: string): string {
  return STAGE_LABEL[s] ?? s;
}

interface Rule {
  re: RegExp;
  fmt: (m: RegExpMatchArray) => string;
}

const RULES: Rule[] = [
  {
    re: /^player\s+(\d+)\s+state\s+(\S+)\s+cannot act$/i,
    fmt: (m) => `${m[1]} 号位状态为「${localizeState(m[2])}」,无法行动`,
  },
  {
    re: /^not your turn \(active=(\d+),\s*you=(\d+)\)$/i,
    fmt: (m) => `现在不是你的回合(当前轮到 ${m[1]} 号位)`,
  },
  {
    re: /^cannot apply action in stage\s+(\S+)$/i,
    fmt: (m) => `当前阶段「${localizeStage(m[1])}」无法行动`,
  },
  {
    re: /^cannot check, must call\s+(\d+)$/i,
    fmt: (m) => `无法过牌,需跟注 ${m[1]}`,
  },
  {
    re: /^raise target (\d+) must exceed current bet (\d+)$/i,
    fmt: (m) => `加注目标 ${m[1]} 必须高于当前下注 ${m[2]}`,
  },
  {
    re: /^not enough chips:\s*need\s+(\d+),\s*have\s+(\d+)$/i,
    fmt: (m) => `筹码不足:需要 ${m[1]},持有 ${m[2]}`,
  },
  {
    re: /^min raise size is (\d+), got (\d+)$/i,
    fmt: (m) => `最小加注幅度为 ${m[1]},提交了 ${m[2]}`,
  },
  {
    re: /^cannot advance from stage\s+(\S+)$/i,
    fmt: (m) => `当前阶段「${localizeStage(m[1])}」无法推进`,
  },
  {
    re: /^unknown action type\s+(\S+)$/i,
    fmt: (m) => `未知动作类型:${m[1]}`,
  },
  {
    re: /^dealer seat (\d+) not in players$/i,
    fmt: (m) => `庄家位 ${m[1]} 不在玩家中`,
  },
  {
    re: /^invalid blinds sb=(\d+) bb=(\d+)$/i,
    fmt: (m) => `盲注无效(小盲 ${m[1]}/大盲 ${m[2]})`,
  },
  { re: /^no chips to all-in$/i, fmt: () => '没有筹码可全下' },
  { re: /^betting round still open$/i, fmt: () => '下注轮尚未结束' },
  { re: /^no players in hand$/i, fmt: () => '本手没有玩家' },
  { re: /^need at least 2 players$/i, fmt: () => '至少需要 2 名玩家' },
  {
    re: /^need at least 2 active players \(with chips\)$/i,
    fmt: () => '至少需要 2 名有筹码的玩家',
  },
  { re: /^hand already started$/i, fmt: () => '本手已经开始' },
  { re: /^roomId and userId required$/i, fmt: () => '需要房间号和用户 ID' },
  { re: /^empty emoji$/i, fmt: () => '表情为空' },
  { re: /^slow down$/i, fmt: () => '操作太频繁,请稍候' },
  { re: /^invalid blinds$/i, fmt: () => '盲注无效' },
  { re: /^maxSeats must be 2\.\.9$/i, fmt: () => '座位数必须在 2-9 之间' },
  {
    re: /^durationMinutes must be 0\.\.1440$/i,
    fmt: () => '时长必须在 0-1440 分钟之间',
  },
  { re: /^password too long$/i, fmt: () => '密码过长' },
  { re: /^room already exists$/i, fmt: () => '房间已存在' },
  { re: /^auth:\s*(.*)$/i, fmt: (m) => `认证失败:${m[1]}` },
];

function translateMessage(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed) return '';
  for (const r of RULES) {
    const m = trimmed.match(r.re);
    if (m) return r.fmt(m);
  }
  return trimmed;
}

export function translateServerError(payload: { code?: string; message?: string } | undefined): string {
  if (!payload) return '未知错误';
  const raw = (payload.message || '').trim();
  if (raw) {
    const translated = translateMessage(raw);
    if (translated && translated !== raw) return translated;
    // Couldn't translate message — fall back to the code label if available.
    const codeLabel = payload.code ? CODE_LABEL[payload.code] : '';
    if (codeLabel) return `${codeLabel}:${raw}`;
    return raw;
  }
  if (payload.code && CODE_LABEL[payload.code]) return CODE_LABEL[payload.code];
  return payload.code || '未知错误';
}
