"use client";

import { Suspense, useEffect, useRef } from "react";
import Image from "next/image";
import { useSearchParams, useRouter } from "next/navigation";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { sanitizeNextUrl, useAuthStore } from "@multica/core/auth";
import {
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import {
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@multica/core/paths";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { CliCallbackError } from "@/features/auth/cli-callback-error";
import { redirectToCliCallback, validateCliCallback } from "@multica/views/auth";

/**
 * Pick where a logged-in user with no explicit `?next=` should land.
 * Un-onboarded users with pending invitations on their email get routed to
 * the batch /invitations page; everyone else falls through to the standard
 * resolver. A network blip on listMyInvitations is non-fatal — we fall
 * through rather than trap the user on an error screen.
 */
async function resolveLoggedInDestination(
  qc: QueryClient,
  hasOnboarded: boolean,
  workspaces: Workspace[],
): Promise<string> {
  if (!hasOnboarded) {
    try {
      const invites = await api.listMyInvitations();
      if (invites.length > 0) {
        qc.setQueryData(workspaceKeys.myInvitations(), invites);
        return paths.invitations();
      }
    } catch {
      // fall through
    }
  }
  return resolvePostAuthDestination(workspaces, hasOnboarded);
}

function LoginPageContent() {
  const router = useRouter();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const searchParams = useSearchParams();

  const cliCallbackRaw = searchParams.get("cli_callback");
  const invalidCliCallback = cliCallbackRaw !== null && !validateCliCallback(cliCallbackRaw);
  // `next` carries a protected URL the user was originally headed to
  // (e.g. /invite/{id}). With URL-driven workspaces there is no legacy
  // "/issues" default — if `next` is absent we decide after login based on
  // the user's workspace list. Sanitize first so a crafted `?next=https://evil`
  // cannot bounce the user off-origin after a successful login.
  const nextUrl = sanitizeNextUrl(searchParams.get("next"));

  const hasOnboarded = useHasOnboarded();
  const cliState = searchParams.get("cli_state") ?? "";
  const authorizingCli = cliCallbackRaw !== null && !invalidCliCallback && !!user;

  // Latched once auth has been observed settled as logged-out on this page.
  // Any `user` that appears afterwards came from the login form in this
  // session — not from an existing session found on arrival.
  const settledLoggedOutRef = useRef(false);

  // Already authenticated ON ARRIVAL — honor ?next= or fall back to first
  // workspace (or /onboarding if the user has none). Skip this entire path
  // when the user arrived to authorize the CLI.
  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      settledLoggedOutRef.current = true;
      return;
    }
    if (cliCallbackRaw !== null) return;
    // A user that appears after this page settled as logged-out is not an
    // arrival. Feishu returns as a fresh page load with the cookie already
    // set, so only that arrival path redirects.
    if (settledLoggedOutRef.current) return;
    if (nextUrl) {
      router.replace(nextUrl);
      return;
    }
    // Fetch instead of reading the cache: on a fresh page load the cache is
    // cold, and `getQueryData() ?? []` would misroute a user who does have
    // workspaces to /workspaces/new. On fetch failure fall back to [] —
    // same destination the cold-cache read produced, rather than trapping
    // the user on the login page.
    void qc
      .ensureQueryData(workspaceListOptions())
      .catch(() => [] as Workspace[])
      .then((list) => resolveLoggedInDestination(qc, hasOnboarded, list))
      .then((dest) => router.replace(dest));
  }, [isLoading, user, router, nextUrl, cliCallbackRaw, hasOnboarded, qc]);

  // The CLI is waiting on a local callback. A session that already exists
  // must hand it a token; showing the login form again never completes setup.
  useEffect(() => {
    if (isLoading || !user || !cliCallbackRaw || invalidCliCallback) return;
    let cancelled = false;
    void api
      .issueCliToken()
      .then((res) => {
        if (!cancelled) redirectToCliCallback(cliCallbackRaw, res.token, cliState);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [isLoading, user, cliCallbackRaw, invalidCliCallback, cliState]);

  if (invalidCliCallback) return <CliCallbackError />;

  return <FeishuSignIn authorizingCli={authorizingCli} showForm={!isLoading && !user} />;
}

function FeishuSignIn({
  authorizingCli,
  showForm,
}: {
  authorizingCli: boolean;
  showForm: boolean;
}) {
  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[#05070b] text-white">
      <Image
        src="/images/landing-bg.webp"
        alt=""
        fill
        preload
        className="object-cover object-center"
        sizes="100vw"
      />
      <div className="relative z-10 mx-4 w-full max-w-md border border-white/20 bg-black/30 px-8 py-12 text-center shadow-[0_24px_80px_rgba(0,0,0,0.28)] backdrop-blur-md">
        <p className="text-body tracking-[0.18em] text-white/75">multica</p>
        {showForm ? (
          <>
            <h1 className="landing-serif mt-3 text-[3.2rem] leading-none tracking-[-0.04em] text-white drop-shadow-[0_10px_34px_rgba(0,0,0,0.32)]">
              登录
            </h1>
            <a
              href="/auth/feishu/start"
              className="mt-10 inline-flex h-12 w-full items-center justify-center bg-white text-body font-semibold text-[#0a0d12] transition-colors hover:bg-white/92"
            >
              飞书登录
            </a>
          </>
        ) : authorizingCli ? (
          <p className="mt-10 text-body text-white/80">正在把登录交给这台电脑</p>
        ) : null}
      </div>
    </div>
  );
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <LoginPageContent />
    </Suspense>
  );
}
