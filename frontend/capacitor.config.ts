import type {CapacitorConfig} from '@capacitor/cli'

const config: CapacitorConfig = {
  appId: 'com.gomental.app',
  appName: 'GoMental',
  webDir: 'dist-mobile',
  android: {
    minWebViewVersion: 83,
  },
}

export default config
