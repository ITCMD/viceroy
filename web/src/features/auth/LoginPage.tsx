import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { Button, Field, FormError } from "@/components/ui";
import { api } from "@/lib/api";
import { useRefreshSession } from "@/lib/session";
import { AuthLayout } from "./AuthLayout";

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const refresh = useRefreshSession();
  const navigate = useNavigate();
  const login = useMutation({
    mutationFn: () => api.post("/auth/login", { email, password }),
    onSuccess: async () => {
      await refresh();
      navigate({ to: "/" });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };

  return (
    <AuthLayout title="Sign in to Viceroy">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Email" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
        <Field label="Password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        <FormError error={login.error} />
        <Button type="submit" loading={login.isPending}>
          Sign in
        </Button>
      </form>
    </AuthLayout>
  );
}
