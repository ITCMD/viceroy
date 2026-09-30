import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { Button, Field, FormError } from "@/components/ui";
import { api } from "@/lib/api";
import { useRefreshSession } from "@/lib/session";
import { AuthLayout } from "./AuthLayout";

export function SetupPage() {
  const [form, setForm] = useState({ name: "", email: "", password: "", confirm: "", household_name: "" });
  const [localError, setLocalError] = useState<string | null>(null);
  const refresh = useRefreshSession();
  const navigate = useNavigate();
  const setup = useMutation({
    mutationFn: () =>
      api.post("/setup", { name: form.name, email: form.email, password: form.password, household_name: form.household_name }),
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
    setup.mutate();
  };

  return (
    <AuthLayout title="Welcome to Viceroy" subtitle="Create the admin account to finish setup.">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Your name" autoComplete="name" required value={form.name} onChange={set("name")} />
        <Field label="Email" type="email" autoComplete="username" required value={form.email} onChange={set("email")} />
        <Field label="Password" type="password" autoComplete="new-password" required minLength={10} hint="At least 10 characters." value={form.password} onChange={set("password")} />
        <Field label="Confirm password" type="password" autoComplete="new-password" required value={form.confirm} onChange={set("confirm")} />
        <Field label="Household name" placeholder="Optional" hint="Budgets are shared within a household." value={form.household_name} onChange={set("household_name")} />
        <FormError error={localError ?? setup.error} />
        <Button type="submit" loading={setup.isPending}>
          Create account
        </Button>
      </form>
    </AuthLayout>
  );
}
