// First-launch consent for the user agreement and privacy policy.
// The fact of consent is persisted to storage; updates to the document
// version bump the key and re-prompt.

const CONSENT_KEY = 'tx_consent_v1';

export function hasConsented(): boolean {
  try {
    return wx.getStorageSync(CONSENT_KEY) === true;
  } catch {
    return false;
  }
}

export function markConsented(): void {
  try {
    wx.setStorageSync(CONSENT_KEY, true);
  } catch {
    // ignore
  }
}

// 模块级未决 promise：多处并发调用 ensureConsent（例如 onShow + onEnterLobby
// 同时触发）时复用同一个 modal，避免微信「同时只能弹一个 modal」导致第二个
// 请求被强制 fail（timeout），进而让 onEnterLobby 拿到 false 直接 return。
let pending: Promise<boolean> | null = null;

/**
 * Show the consent modal. Resolves true when the user accepts, false when they
 * decline. Calling this when consent is already given resolves true immediately.
 * 并发安全：未决期间的二次调用复用同一个 promise。
 */
export function ensureConsent(): Promise<boolean> {
  if (hasConsented()) return Promise.resolve(true);
  if (pending) return pending;
  pending = new Promise((resolve) => {
    wx.showModal({
      title: '欢迎来到德州扑克',
      content:
        '本游戏为娱乐用途，所有筹码均为虚拟道具，不可与现金或任何形式的财物互兑。\n\n' +
        '继续使用即表示您已阅读并同意《用户协议》和《隐私政策》。',
      confirmText: '同意进入',
      cancelText: '退出',
      success: (res) => {
        pending = null;
        if (res.confirm) {
          markConsented();
          resolve(true);
        } else {
          resolve(false);
        }
      },
      fail: (err) => {
        console.warn('[consent] showModal fail', err);
        pending = null;
        resolve(false);
      },
    });
  });
  return pending;
}
