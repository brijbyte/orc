import { useMemo } from "react";
import type { ThemePref } from "../lib/theme";
import { SettingsSection } from "./SettingsSection";
import { useSettings } from "./SettingsContext";
import s from "./GeneralSettings.module.css";

export function GeneralSettings({
  model,
  effort,
  onModelChange,
  onEffortChange,
}: {
  model: string;
  effort: string;
  onModelChange: (model: string) => void;
  onEffortChange: (effort: string) => void;
}) {
  const { models, theme, setTheme } = useSettings();
  const efforts = useMemo(() => {
    const values = models.find((item) => item.slug === model)?.efforts ?? [
      "low",
      "medium",
      "high",
    ];
    return values.includes(effort) || !effort ? values : [effort, ...values];
  }, [models, model, effort]);
  const modelOptions = models.some((item) => item.slug === model)
    ? models
    : model
      ? [{ slug: model, name: model, description: "" }, ...models]
      : models;

  const selectModel = (nextModel: string) => {
    const nextEfforts = models.find((item) => item.slug === nextModel)
      ?.efforts ?? ["low", "medium", "high"];
    onModelChange(nextModel);
    if (!nextEfforts.includes(effort))
      onEffortChange(
        nextEfforts.includes("medium") ? "medium" : (nextEfforts[0] ?? effort),
      );
  };

  return (
    <SettingsSection>
      <label className={s.setting}>
        <span>default model</span>
        <select
          value={model}
          onChange={(event) => selectModel(event.target.value)}
        >
          {modelOptions.map((item) => (
            <option key={item.slug} value={item.slug}>
              {item.name || item.slug}
            </option>
          ))}
        </select>
      </label>
      <label className={s.setting}>
        <span>default effort</span>
        <select
          value={effort}
          onChange={(event) => onEffortChange(event.target.value)}
        >
          {efforts.map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </select>
      </label>
      <p className={s.hint}>applies to new sessions.</p>
      <label className={s.setting}>
        <span>theme</span>
        <select
          value={theme}
          onChange={(event) => setTheme(event.target.value as ThemePref)}
        >
          <option value="system">system</option>
          <option value="light">light</option>
          <option value="dark">dark</option>
        </select>
      </label>
    </SettingsSection>
  );
}
