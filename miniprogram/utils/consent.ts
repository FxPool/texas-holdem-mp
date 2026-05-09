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

/**
 * Show the consent modal. Resolves true when the user accepts, false when they
 * decline. Calling this when consent is already given resolves true immediately.
 */
export function ensureConsent(): Promise<boolean> {
  if (hasConsented()) return Promise.resolve(true);
  return new Promise((resolve) => {
    wx.showModal({
      title: '欢迎来到德州扑克',
      content:
        '本游戏为娱乐用途，所有筹码均为虚拟道具，不可与现金或任何形式的财物互兑。\n\n' +
        '继续使用即表示您已阅读并同意《用户协议》和《隐私政策》。',
      confirmText: '同意并继续',
      cancelText: '退出',
      success: (res) => {
        if (res.confirm) {
          markConsented();
          resolve(true);
        } else {
          resolve(false);
        }
      },
      fail: () => resolve(false),
    });
  });
}
