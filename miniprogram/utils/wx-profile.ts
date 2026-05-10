// 微信资料采集相关：把 chooseAvatar 给的临时路径持久化到用户数据目录，
// 并提供 isUrlAvatar 判定，让渲染层在 emoji 与图片之间切换。

const SAVED_AVATAR_KEY = 'tx_wx_avatar_local';

/** 判断头像字段是图片地址（http/wxfile/本地文件路径）而不是 emoji。 */
export function isUrlAvatar(s: string | undefined | null): boolean {
  if (!s) return false;
  // emoji 不包含斜杠或冒号；任何 URL/本地路径都会包含其中之一。
  return s.indexOf('/') >= 0 || s.indexOf(':') >= 0;
}

/**
 * 持久化 chooseAvatar 返回的临时文件，返回可长期使用的本地路径。
 * 失败时直接返回原路径（仍可在当前会话内使用）。
 */
export function persistAvatar(tempFilePath: string): Promise<string> {
  return new Promise((resolve) => {
    if (!tempFilePath) {
      resolve('');
      return;
    }
    try {
      const fs = wx.getFileSystemManager();
      fs.saveFile({
        tempFilePath,
        success: (res) => {
          const saved = res.savedFilePath;
          if (saved) {
            try {
              wx.setStorageSync(SAVED_AVATAR_KEY, saved);
            } catch {
              // ignore
            }
            resolve(saved);
          } else {
            resolve(tempFilePath);
          }
        },
        fail: (err) => {
          console.warn('[wx-profile] saveFile failed', err);
          resolve(tempFilePath);
        },
      });
    } catch (e) {
      console.warn('[wx-profile] saveFile threw', e);
      resolve(tempFilePath);
    }
  });
}

/** 上次保存的本地头像路径（可能不存在）。 */
export function lastSavedAvatar(): string {
  try {
    return (wx.getStorageSync(SAVED_AVATAR_KEY) as string) || '';
  } catch {
    return '';
  }
}
