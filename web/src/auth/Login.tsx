import { FormEvent, useState } from "react";
import { api } from "../lib/api";
import { Button } from "../ui/Button";
import s from "./Login.module.css";

export function Login({ onLogin }: { onLogin: () => void }) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(false);
    api
      .login(password)
      .then(onLogin)
      .catch(() => setError(true))
      .finally(() => setBusy(false));
  };

  return (
    <main className={s.page}>
      <form className={s.form} onSubmit={submit}>
        <div className={s.mark}>🧌</div>
        <h1>orc</h1>
        <label htmlFor="password">web password</label>
        <input
          id="password"
          type="password"
          autoComplete="current-password"
          autoFocus
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
        {error && <div className={s.error}>incorrect password</div>}
        <Button
          outline
          type="submit"
          className={s.submit}
          disabled={busy || !password}
        >
          {busy ? "signing in…" : "sign in"}
        </Button>
      </form>
    </main>
  );
}
