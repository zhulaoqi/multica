"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { AdminPage } from "@multica/views/operator";

/**
 * Platform console. Gated on the server-set `is_operator` flag from
 * `/api/me`; the overview endpoint behind the page rejects everyone else too,
 * so this guard only decides where to send them.
 */
export default function AdminRoute() {
  const router = useRouter();
  const user = useAuthStore((state) => state.user);
  const isLoading = useAuthStore((state) => state.isLoading);

  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      router.replace("/login?next=/admin");
      return;
    }
    if (!user.is_operator) router.replace("/");
  }, [isLoading, user, router]);

  if (!user?.is_operator) return null;
  return <AdminPage />;
}
