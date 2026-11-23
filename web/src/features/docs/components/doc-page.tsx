import Link from "next/link";

import { Logo } from "@/components/brand/logo";
import { parseMarkdown } from "../markdown";
import { MarkdownView } from "./markdown-view";

// A page of the documentation: readable by anybody, signed in or not, so it stands outside
// the application's shell, like the legal pages. The text is a markdown file of the
// repository, read here, at build time, and the same file the repository shows on GitHub.
export function DocPage({ title, source }: { title: string; source: string }) {
  const blocks = parseMarkdown(source);

  return (
    <main className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-8 px-4 py-16">
      <Link href="/" aria-label="Ballast, home" className="flex w-fit items-center gap-2.5">
        <Logo className="text-brand" />
        <span className="text-[15px] font-medium tracking-wide">Ballast</span>
      </Link>

      <header>
        <h1 className="text-3xl font-normal tracking-tight">{title}</h1>
      </header>

      <MarkdownView blocks={blocks} />

      <footer className="flex flex-wrap gap-x-5 gap-y-2 border-t pt-6 text-sm text-muted-foreground">
        <Link href="/privacy" className="hover:text-foreground">Privacy</Link>
        <Link href="/terms" className="hover:text-foreground">Terms</Link>
        <a href="https://github.com/myballast/ballast" className="hover:text-foreground">GitHub</a>
      </footer>
    </main>
  );
}
