import { Box } from "lucide-react";
import codex from "../assets/providers/codex.svg";
import s from "./ProviderIcon.module.css";

const icons: Record<string, string> = { codex };

export function ProviderIcon({
  provider,
  size = 18,
  className,
}: {
  provider?: string;
  size?: number;
  className?: string;
}) {
  const src = provider ? icons[provider.toLowerCase()] : undefined;
  if (!src)
    return (
      <Box className={className} size={size} strokeWidth={1.6} aria-hidden />
    );
  return (
    <img
      className={[s.icon, className].filter(Boolean).join(" ")}
      src={src}
      width={size}
      height={size}
      alt=""
      aria-hidden
    />
  );
}
