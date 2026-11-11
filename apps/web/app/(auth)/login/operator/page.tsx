"use client";

import { useState } from "react";
import Image from "next/image";
import Link from "next/link";
import { setLoggedInCookie } from "@/features/auth/auth-cookie";

export default function OperatorLoginPage() {
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setPending(true);
    setError("");
    try {
      const response = await fetch("/auth/operator", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, password }),
      });
      if (!response.ok) {
        setError(response.status === 503 ? "开发者登录还没配置" : "账户名或口令不对");
        return;
      }
      setLoggedInCookie();
      window.location.assign("/login");
    } catch {
      setError("登录没有发出去");
    } finally {
      setPending(false);
    }
  }

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
      <form
        onSubmit={submit}
        className="relative z-10 mx-4 w-full max-w-md border border-white/20 bg-black/30 px-8 py-12 text-center shadow-[0_24px_80px_rgba(0,0,0,0.28)] backdrop-blur-md"
      >
        <p className="text-body tracking-[0.18em] text-white/75">multica</p>
        <h1 className="landing-serif mt-3 text-[3.2rem] leading-none tracking-[-0.04em] text-white">
          开发者
        </h1>
        <label className="mt-10 block text-left text-body text-white/80">
          账户名
          <input
            type="text"
            required
            autoComplete="username"
            value={name}
            onChange={(event) => setName(event.target.value)}
            className="mt-2 h-12 w-full border border-white/25 bg-black/30 px-3 text-white outline-none"
          />
        </label>
        <label className="mt-4 block text-left text-body text-white/80">
          口令
          <input
            type="password"
            required
            autoComplete="current-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            className="mt-2 h-12 w-full border border-white/25 bg-black/30 px-3 text-white outline-none"
          />
        </label>
        {error ? <p className="mt-4 text-body text-white">{error}</p> : null}
        <button
          type="submit"
          disabled={pending}
          className="mt-8 inline-flex h-12 w-full items-center justify-center bg-white text-body font-semibold text-[#0a0d12] disabled:opacity-60"
        >
          登录
        </button>
        <Link href="/login" className="mt-6 inline-block text-body text-white/75 underline underline-offset-4">
          返回飞书登录
        </Link>
      </form>
    </div>
  );
}
