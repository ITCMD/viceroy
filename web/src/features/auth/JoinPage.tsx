import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { Button, Field, FormError } from "@/components/ui";
import { api } from "@/lib/api";
import { useRefreshSession } from "@/lib/session";
import type { InviteInfo } from "@/features/household/api";
import { AuthLayout } from "./AuthLayout";

/** /join/$token: a household invite (make an account) or a password reset link. */
export function JoinPage() {
  const { token } = useParams({ strict: false }) as { token: string };
  const info = useQuery({ queryKey: ["invite", token], queryFn: () => api.get<InviteInfo>(`/invites/${token}`), retry: false });
  const [form, setForm] = useState({ name: "", email: "", password: "", confirm: "" });
  const [localError, setLocalError] = useState<string | null>(null);
  const refresh = useRefreshSession();
  const navigate = useNavigate();
  const accept = useMutation({
    mutationFn: () => api.post(`/invites/${token}/accept`, info.data?.kind === "reset" ? { password: form.password } : { name: form.name, email: form.email, password: form.password }),
    onSuccess: async () => {
      await refresh();
      navigate({ to: "/" });
    },
  });
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (form.password !== form.confirm) {
      setLocalError("Passwords don't match.");
      return;
    }
    setLocalError(null);
    accept.mutate();
  };

  if (info.isPending) return <AuthLayout title="Loading…">{null}</AuthLayout>;
  if (info.isError)
    return (
      <AuthLayout title="This link doesn't work">
        <div className="flex flex-col gap-4">
          <FormError error={info.error} />
          <Link to="/login" className="text-center text-sm font-medium text-accent hover:underline">
            Go to sign in
          </Link>
        </div>
      </AuthLayout>
    );
  const reset = info.data.kind === "reset";
  return (
    <AuthLayout
      title={reset ? "Choose a new password" : `Join ${info.data.household_name}`}
      subtitle={reset ? `For ${info.data.email}. Signing in elsewhere will stop working.` : `${info.data.invited_by || "An admin"} invited you to share their budget on Viceroy.`}
    >
      <form onSubmit={submit} className="flex flex-col gap-4">
        {reset ? (
          <input type="email" autoComplete="username" value={info.data.email} readOnly hidden />
        ) : (
          <>
            <Field label="Your name" autoComplete="name" required value={form.name} onChange={set("name")} />
            <Field label="Email" type="email" autoComplete="username" required value={form.email} onChange={set("email")} />
          </>
        )}
        <Field label={reset ? "New password" : "Password"} type="password" autoComplete="new-password" required minLength={10} hint="At least 10 characters." value={form.password} onChange={set("password")} />
        <Field label="Confirm password" type="password" autoComplete="new-password" required value={form.confirm} onChange={set("confirm")} />
        <FormError error={localError ?? accept.error} />
        <Button type="submit" loading={accept.isPending}>
          {reset ? "Set password" : "Join household"}
        </Button>
      </form>
    </AuthLayout>
  );
}
