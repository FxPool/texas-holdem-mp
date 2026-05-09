import { store } from './utils/store';
import { ensureLoggedIn } from './utils/auth';
import * as sfx from './utils/sfx';

App<IAppOption>({
  globalData: {
    version: '0.1.0',
  },
  onLaunch() {
    store.init();
    sfx.loadMuted();
    const sysInfo = wx.getSystemInfoSync();
    store.setSystemInfo({
      windowWidth: sysInfo.windowWidth,
      windowHeight: sysInfo.windowHeight,
      pixelRatio: sysInfo.pixelRatio,
      safeAreaTop: sysInfo.safeArea?.top ?? 0,
      safeAreaBottom: sysInfo.screenHeight - (sysInfo.safeArea?.bottom ?? sysInfo.screenHeight),
    });
    // Warm up the session token in the background so it's ready by the time
    // the user enters a table. Non-fatal on failure (table page retries).
    ensureLoggedIn().catch((err) => {
      console.warn('[app] ensureLoggedIn at launch failed', err);
    });
  },
});

interface IAppOption {
  globalData: {
    version: string;
  };
  onLaunch?: () => void;
}
