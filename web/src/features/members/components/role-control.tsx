"use client";

import { useState, useTransition } from "react";

import { changeRole } from "../actions";
import type { Role } from "../types";

// RoleControl is the role of a member, changeable by an admin. A native select:
// it is keyboard- and screen-reader-accessible as it stands, and the design's
// button with a caret is what a select looks like.
//
// When the role cannot be changed the select is disabled, and the sentence that
// says why is its tooltip and is read after its label. The tooltip sits on the
// wrapper: a disabled control shows none of its own.
export function RoleControl({
  projectId,
  userId,
  name,
  role,
  options,
  lockedBecause,
}: {
  projectId: string;
  userId: string;
  name: string;
  role: Role;
  // The roles the caller may give.
  options: Role[];
  lockedBecause: string | null;
}) {
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const reason = `role-reason-${userId}`;

  return (
    <div className="flex flex-col gap-1" title={lockedBecause ?? undefined}>
      <select
        aria-label={`Role for ${name}`}
        aria-describedby={lockedBecause ? reason : undefined}
        value={role}
        disabled={lockedBecause !== null || pending}
        onChange={(event) => {
          const next = event.target.value as Role;
          setError(null);
          startTransition(async () => {
            const outcome = await changeRole(projectId, userId, next);
            if (outcome.error) setError(outcome.error);
          });
        }}
        className="data h-7 w-full rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-60"
      >
        {/* The member's own role is always listed, even when the caller could
            not give it: a select that does not show the current value shows
            the wrong one. */}
        {Array.from(new Set([...options, role])).map((option) => (
          <option key={option} value={option} disabled={!options.includes(option)}>
            {option}
          </option>
        ))}
      </select>
      {lockedBecause && (
        <span id={reason} className="sr-only">
          {lockedBecause}
        </span>
      )}
      {error && (
        <span role="alert" className="text-[11px] text-muted-foreground">
          {error}
        </span>
      )}
    </div>
  );
}
