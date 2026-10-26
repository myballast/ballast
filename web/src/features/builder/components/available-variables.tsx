import { KeyRound } from "lucide-react";

import type { Available } from "../variables";

// AvailableVariables lists what a step can read: the variables written by the steps
// that lead to it, and only those. One bound to a secret is marked as such and
// shows its name alone — the graph holds the secret's key, so there is no value to
// show, here or anywhere.
export function AvailableVariables({ available }: { available: Available[] }) {
  return (
    <section aria-labelledby="available-variables" className="flex flex-col gap-1.5">
      <h3 id="available-variables" className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
        Available variables
      </h3>

      {available.length === 0 ? (
        <p className="text-xs text-muted-foreground">Nothing is defined before this step.</p>
      ) : (
        <ul className="flex flex-wrap gap-1.5">
          {available.map((variable) => (
            <li
              key={variable.name}
              className="data flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-xs text-muted-foreground"
            >
              {variable.name}
              {variable.secret && (
                <span className="flex items-center gap-0.5 text-[10px]">
                  <KeyRound aria-hidden className="size-3" />
                  secret
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
