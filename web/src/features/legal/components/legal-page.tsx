import Link from "next/link";

import { Logo } from "@/components/brand/logo";

// The frame of a legal page: readable by anybody, signed in or not, so it stands
// outside the application's shell. The text is set by the page that uses it.
export function LegalPage({
  title,
  updated,
  children,
}: {
  title: string;
  updated: string;
  children: React.ReactNode;
}) {
  return (
    <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col gap-8 px-4 py-16">
      <Link href="/" aria-label="Ballast, home" className="flex w-fit items-center gap-2.5">
        <Logo className="text-brand" />
        <span className="text-[15px] font-medium tracking-wide">Ballast</span>
      </Link>

      <header className="flex flex-col gap-1.5">
        <h1 className="text-3xl font-normal tracking-tight">{title}</h1>
        <p className="text-sm text-muted-foreground">Last updated {updated}</p>
      </header>

      <div className="flex flex-col gap-8 text-sm leading-relaxed">{children}</div>

      <footer className="flex flex-wrap gap-x-5 gap-y-2 border-t pt-6 text-sm text-muted-foreground">
        <Link href="/privacy" className="hover:text-foreground">Privacy</Link>
        <Link href="/terms" className="hover:text-foreground">Terms</Link>
        <a href={`mailto:${contactAddress}`} className="hover:text-foreground">{contactAddress}</a>
      </footer>
    </main>
  );
}

export function LegalSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-base font-medium">{title}</h2>
      {children}
    </section>
  );
}

export function LegalList({ children }: { children: React.ReactNode }) {
  return <ul className="flex list-disc flex-col gap-1.5 pl-5">{children}</ul>;
}

// One address for support and for every request about personal data.
export const contactAddress = "contact@myballast.pro";

export function ContactLink() {
  return (
    <a href={`mailto:${contactAddress}`} className="text-brand hover:underline">
      {contactAddress}
    </a>
  );
}
