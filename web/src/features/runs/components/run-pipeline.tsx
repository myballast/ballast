import { ExternalLink } from "lucide-react";

import { ciFacts, jobLink } from "../ci";
import type { RunCI } from "../types";

// RunPipeline says what launched the run: which CI, which repository, branch and
// commit, and the link to the job.
//
// It is declared by whoever launched the run and proved to Ballast by nobody, so it
// is not drawn as a fact the way the run's own are: it carries the word "pipeline"
// and a note that says so. Everything in it is text, rendered by React as text and
// nothing else; the one thing that can be followed is the link to the job, and only
// if it is an https address (jobLink), opened in a tab of its own with no access
// back to this page.
//
// Neutral on purpose: it is not a status, and violet is for actions.
export function RunPipeline({ ci }: { ci: RunCI }) {
  const facts = ciFacts(ci);
  const link = jobLink(ci.job_url);

  return (
    <div
      aria-label="Pipeline"
      title="Declared by the pipeline that launched the run. Ballast does not verify it."
      className="flex flex-wrap items-center gap-x-2 gap-y-1.5"
    >
      <span className="text-[11px] tracking-wider text-muted-foreground uppercase">Pipeline</span>

      <ul className="flex flex-wrap gap-1.5">
        {facts.map((fact) => (
          <li key={fact.key} className="data flex min-w-0 gap-1.5 rounded-md border px-2 py-0.5 text-[11px]">
            <span className="shrink-0 text-muted-foreground">{fact.key}:</span>
            {/* A branch or a repository can be long, and is somebody else's text:
                it is cut to fit, and shown whole on hover. */}
            <span className="max-w-[18rem] truncate" title={fact.value}>
              {fact.value}
            </span>
          </li>
        ))}
      </ul>

      {link && (
        <a
          href={link.href}
          target="_blank"
          rel="noopener noreferrer"
          className="data inline-flex items-center gap-1 text-[11px] text-brand hover:underline"
        >
          <ExternalLink aria-hidden className="size-3" />
          View job
          <span className="text-muted-foreground">on {link.host}</span>
          <span className="sr-only"> (opens in a new tab)</span>
        </a>
      )}
    </div>
  );
}
