import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type WishSort = "added" | "price" | "score";

export type WishItem = {
  id: number;
  title: string;
  url: string;
  store: string;
  price_cents: number | null;
  price_source: "" | "fetched" | "user";
  image_url: string;
  stars: number;
  saves_money: boolean;
  notes: string;
  wanted_by: number[];
  added_by: number | null;
  created_at: number;
  score: number;
  score_text: string;
  afford: "" | "now" | "month_end";
  bought_at: string | null;
  bought_txn_id: number | null;
  txn_name: string;
  txn_amount: number;
  txn_date: string;
};

export type Afford = {
  goal_id: number;
  goal_name: string;
  goal_icon: string;
  saved_now: number;
  month_end: number;
  month_budget: number;
  month_contributed: number;
  on_budget: boolean;
  month_end_date: string;
  month: string;
};

export type Member = { id: number; name: string };

export type Wishlist = { items: WishItem[]; bought: WishItem[]; members: Member[]; afford: Afford; sort: WishSort };

export type Preview = { url: string; store: string; title: string; price_cents: number | null; image_url: string; blocked: boolean; /** The store answered with an error page; nothing was read. */ refused: boolean };

export const wishlistQuery = (sort: WishSort, person: number) =>
  queryOptions({
    queryKey: ["wishlist", sort, person],
    queryFn: () => api.get<Wishlist>(`/wishlist?sort=${sort}${person ? `&person=${person}` : ""}`),
  });

/** A mutation that refreshes the wishlist and everything a purchase touches (goals, budget). */
export function useWishMutation<V = void>(fn: (v: V) => Promise<unknown>, onSuccess?: () => void) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess,
    onSettled: () => Promise.all(["wishlist", "goals", "budget", "transactions"].map((k) => qc.invalidateQueries({ queryKey: [k] }))),
  });
}
