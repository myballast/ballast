"use client";

import { ShieldAlert, ShieldCheck, ShieldQuestion, ShieldX, X } from "lucide-react";
import Link from "next/link";
import { useRef, useState, useTransition } from "react";

import { Button } from "@/components/ui/button";
import { Time } from "@/components/time";

import { loadChallenge, verifyEnvironment, type VerifyState } from "../actions";
import type { Environment, ProofFailure, VerificationChallenge } from "../types";
import { CopyButton } from "./copy-button";

// What the line under the table says, by where the target stands. Neutral text
// with an icon: the colours of a status are for runs.
const standing = {
  pending: { label: "Pending verification", icon: ShieldAlert },
  verification_failed: { label: "Verification failed", icon: ShieldX },
  proof_ok: { label: "Challenge served · awaiting approval", icon: ShieldQuestion },
  verified: { label: "Verified", icon: ShieldCheck },
} as const;

// What a failed attempt found, in words: the status the target answered and how
// the file differed. Never what it said.
function found(failure: ProofFailure): string {
  if (failure.too_long) return "a file longer than 4 KiB, which cannot be the token";
  if (failure.status === undefined) return "nothing: the target gave no answer";
  if (failure.bytes !== undefined) return `a file of ${failure.bytes} bytes that is not the token`;
  return "no file";
}

function Row({ name, value, copy }: { name: string; value: string; copy?: string }) {
  return (
    <div className="grid grid-cols-[6rem_minmax(0,1fr)_auto] items-center border-b last:border-b-0">
      <div className="data border-r px-3 py-2 text-[11.5px] text-muted-foreground">{name}</div>
      <div className="data truncate px-3 py-2 text-[11.5px]" title={value}>
        {value}
      </div>
      <div className="px-1">{copy !== undefined && <CopyButton value={copy} label={name} />}</div>
    </div>
  );
}

// VerifyDialog is the "Verify" of a row: what to publish, when Ballast last
// looked, and the button that makes it look now. It follows the design's
// "Verify domain ownership": a short statement, the record to publish, the state
// with its date, Cancel and Run challenge.
//
// The record is a file at the root of the target's origin, not a DNS entry: that
// is how Ballast proves it, and it is re-checked before every run all the same.
// The token is asked for when the dialog opens and held here; the table that
// holds the button carries none.
export function VerifyDialog({
  projectId,
  environment,
  now,
}: {
  projectId: string;
  environment: Environment;
  now: Date;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [challenge, setChallenge] = useState<VerificationChallenge | null>(null);
  const [asked, setAsked] = useState(false);
  const [state, setState] = useState<VerifyState>({});
  const [read, startReading] = useTransition();
  const [running, startRunning] = useTransition();
  // When the page was drawn, until a check is made and the moment is the click's.
  const [at, setAt] = useState(now);

  const host = new URL(environment.base_url).host;
  const status = challenge?.verification_status ?? environment.verification_status;
  const { label, icon: Icon } = standing[status];
  const checked = challenge?.checked_at ?? environment.verification_checked_at;

  function open() {
    setState({});
    setAt(new Date());
    dialog.current?.showModal();
    startReading(async () => {
      setChallenge(await loadChallenge(projectId, environment.id));
      setAsked(true);
    });
  }

  function run() {
    startRunning(async () => {
      setState(await verifyEnvironment(projectId, environment.id));
      setChallenge(await loadChallenge(projectId, environment.id));
      setAt(new Date());
    });
  }

  const path = challenge ? new URL(challenge.url).pathname : null;

  return (
    <>
      <button
        type="button"
        onClick={open}
        className="text-[11.5px] text-brand hover:underline"
        aria-label={`Verify ${environment.name}`}
      >
        Verify
      </button>

      <dialog
        ref={dialog}
        aria-labelledby={`verify-${environment.id}`}
        className="m-auto w-[min(35rem,calc(100vw-2rem))] rounded-2xl border bg-card p-0 text-left text-foreground backdrop:bg-background/70"
      >
        <div className="flex items-center border-b px-5 py-4">
          <h2 id={`verify-${environment.id}`} className="text-[15px] font-normal">
            Verify domain ownership
          </h2>
          <button
            type="button"
            onClick={() => dialog.current?.close()}
            className="ml-auto grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <X aria-hidden className="size-4" />
            <span className="sr-only">Close</span>
          </button>
        </div>

        <div className="flex flex-col gap-4 p-5">
          <p className="text-[12.5px] leading-relaxed text-muted-foreground">
            Ballast only generates load against targets you control. Publish the file below on{" "}
            <span className="data">{host}</span>, then run the challenge. Verification is re-checked before every run.
          </p>

          {read && !asked ? (
            <p role="status" className="text-sm text-muted-foreground">
              Reading the challenge…
            </p>
          ) : challenge && path ? (
            <div className="overflow-hidden rounded-lg border">
              <Row name="host" value={host} />
              <Row name="path" value={path.replace(/ballast-.*$/, "ballast-<token>")} copy={path} />
              <Row name="content" value={challenge.token} copy={challenge.token} />
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">
              Only somebody with the maintainer role on this project reads the challenge.
            </p>
          )}

          <p role="status" className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
            <Icon aria-hidden className="size-3.5 shrink-0" />
            <span>{label}</span>·
            <span>{checked ? <>last checked <Time iso={checked} mode="relative" now={at} /></> : "never checked"}</span>
          </p>

          {state.error && <Failure message={state.error} failure={state.failure} />}
          {state.proven && (
            <p role="status" className="text-sm">
              The target served its challenge. An admin of this project still has to approve it before any run can
              reach it.{" "}
              <Link href={`/projects/${projectId}/environments/${environment.id}`} className="text-brand hover:underline">
                Open the environment
              </Link>
            </p>
          )}
        </div>

        <div className="flex justify-end gap-2 border-t px-5 py-3.5">
          <Button variant="outline" onClick={() => dialog.current?.close()}>
            Cancel
          </Button>
          <Button onClick={run} disabled={running || !challenge}>
            {running ? "Checking the target…" : "Run challenge"}
          </Button>
        </div>
      </dialog>
    </>
  );
}

// Failure says what went wrong in three lines — where Ballast looked, what
// answered, what it found — and then the sentence, which says what to fix.
function Failure({ message, failure }: { message: string; failure?: ProofFailure }) {
  if (!failure) {
    return (
      <p role="alert" className="text-sm text-muted-foreground">
        {message}
      </p>
    );
  }

  return (
    <div role="alert" className="flex flex-col gap-2 rounded-lg border px-3 py-2.5 text-sm">
      <dl className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-1 text-[12px]">
        <dt className="text-muted-foreground">Looked at</dt>
        <dd className="data break-all">{failure.searched}</dd>
        <dt className="text-muted-foreground">Answered</dt>
        <dd className="data">{failure.status !== undefined ? `HTTP ${failure.status}` : "no answer"}</dd>
        <dt className="text-muted-foreground">Found</dt>
        <dd>{found(failure)}</dd>
      </dl>
      <p className="text-muted-foreground">{failure.reason}</p>
    </div>
  );
}
