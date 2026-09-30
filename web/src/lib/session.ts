import { queryOptions, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./api";

export type User = { id: number; email: string; name: string; is_admin: boolean };
export type Household = { id: number; name: string };
export type Session = { needs_setup: boolean; user: User | null; household: Household | null };

export const sessionQuery = queryOptions({
  queryKey: ["session"],
  queryFn: () => api.get<Session>("/session"),
  staleTime: 60_000,
});

/** Used by route guards. */
export const loadSession = (qc: QueryClient) => qc.ensureQueryData(sessionQuery);

export function useSession() {
  return useQuery(sessionQuery);
}

/** Forces a refetch even when no component observes the session (e.g. from the
 *  setup/login pages, where only route guards read it), so guards see fresh data. */
export function useRefreshSession() {
  const qc = useQueryClient();
  return () => qc.fetchQuery({ ...sessionQuery, staleTime: 0 });
}
