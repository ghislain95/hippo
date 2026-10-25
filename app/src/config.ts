import Constants from 'expo-constants';

/** Build-time configuration of the app (app.config.ts, `extra`). */
export type AppConfig = {
  apiUrl: string;
  apiToken: string;
};

type Extra = Partial<AppConfig>;

export function readConfig(extra: Extra | undefined = Constants.expoConfig?.extra): AppConfig {
  return {
    apiUrl: extra?.apiUrl ?? 'http://127.0.0.1:8080',
    apiToken: extra?.apiToken ?? '',
  };
}
