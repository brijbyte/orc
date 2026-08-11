import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
} from "react";
import { api } from "../lib/api";
import { setThemePref, themePref, type ThemePref } from "../lib/theme";
import type { Model, NotifyChannel, NotifyType, Settings } from "../lib/types";

type SettingsData = Settings & {
  channels: NotifyChannel[];
  types: NotifyType[];
};

type SettingsContextValue = {
  open: boolean;
  loading: boolean;
  error: string;
  data: SettingsData | null;
  models: Model[];
  theme: ThemePref;
  openDialog: () => void;
  closeDialog: () => void;
  setTheme: (theme: ThemePref) => void;
  save: (
    settings?: Partial<Settings>,
    channels?: NotifyChannel[],
  ) => Promise<void>;
  changePassword: (current: string, next: string) => Promise<void>;
  testChannel: (channel: NotifyChannel) => Promise<void>;
};

const SettingsContext = createContext<SettingsContextValue | null>(null);

export function SettingsProvider({
  models,
  children,
}: {
  models: Model[];
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [data, setData] = useState<SettingsData | null>(null);
  const [theme, setThemeState] = useState<ThemePref>(themePref());

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    setData(null);
    try {
      const [settings, notifications] = await Promise.all([
        api.settings(),
        api.notify(),
      ]);
      setData({
        model: settings.model ?? "",
        effort: settings.effort ?? "",
        types: notifications.types ?? [],
        channels: notifications.channels ?? [],
      });
    } catch {
      setError("cannot load settings");
    } finally {
      setLoading(false);
    }
  }, []);

  const openDialog = useCallback(() => {
    setOpen(true);
    void load();
  }, [load]);

  const closeDialog = useCallback(() => setOpen(false), []);

  const setTheme = useCallback((next: ThemePref) => {
    setThemeState(next);
    setThemePref(next);
  }, []);

  const save = useCallback(
    async (settings?: Partial<Settings>, channels?: NotifyChannel[]) => {
      if (settings) await api.settingsSave(settings);
      if (channels) await api.notifySave(channels);
      setData((current) =>
        current
          ? {
              ...current,
              ...settings,
              ...(channels ? { channels } : {}),
            }
          : current,
      );
    },
    [],
  );

  const value = useMemo<SettingsContextValue>(
    () => ({
      open,
      loading,
      error,
      data,
      models,
      theme,
      openDialog,
      closeDialog,
      setTheme,
      save,
      changePassword: api.changePassword,
      testChannel: api.notifyTest,
    }),
    [
      open,
      loading,
      error,
      data,
      models,
      theme,
      openDialog,
      closeDialog,
      setTheme,
      save,
    ],
  );

  return (
    <SettingsContext.Provider value={value}>
      {children}
    </SettingsContext.Provider>
  );
}

export function useSettings() {
  const value = useContext(SettingsContext);
  if (!value) throw new Error("useSettings must be inside SettingsProvider");
  return value;
}
