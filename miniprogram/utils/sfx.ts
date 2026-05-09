// Sound-effect hook. Plays short MP3 cues from /assets/sfx/<name>.mp3.
// If a file is missing the play() call silently no-ops, so the rest of the
// app keeps working without audio assets shipped.
//
// To enable sounds, drop the following files into miniprogram/assets/sfx/:
//   deal.mp3   - card dealing flick
//   chip.mp3   - chip thunk into pot
//   fold.mp3   - fold tap
//   win.mp3    - winner chime (showdown)
//
// Royalty-free options: Freesound CC0, OpenGameArt, Mixkit.

type Sfx = 'deal' | 'chip' | 'fold' | 'win';

const FILES: Record<Sfx, string> = {
  deal: '/assets/sfx/deal.mp3',
  chip: '/assets/sfx/chip.mp3',
  fold: '/assets/sfx/fold.mp3',
  win: '/assets/sfx/win.mp3',
};

const players: Partial<Record<Sfx, WechatMiniprogram.InnerAudioContext>> = {};
let muted = false;
const MUTE_KEY = 'tx_sfx_muted';

function ensure(name: Sfx): WechatMiniprogram.InnerAudioContext | null {
  if (!wx.createInnerAudioContext) return null;
  if (players[name]) return players[name]!;
  const a = wx.createInnerAudioContext();
  a.src = FILES[name];
  a.autoplay = false;
  a.onError(() => {
    // Asset missing or decode failed — drop the player so we don't keep
    // hammering it. Subsequent calls become silent no-ops.
    players[name] = undefined;
  });
  players[name] = a;
  return a;
}

export function loadMuted(): void {
  try {
    muted = !!wx.getStorageSync(MUTE_KEY);
  } catch {
    muted = false;
  }
}

export function setMuted(v: boolean): void {
  muted = v;
  try {
    wx.setStorageSync(MUTE_KEY, v);
  } catch {
    // ignore
  }
}

export function isMuted(): boolean {
  return muted;
}

export function play(name: Sfx): void {
  if (muted) return;
  const a = ensure(name);
  if (!a) return;
  try {
    a.stop();
    a.play();
  } catch {
    // best-effort
  }
}
