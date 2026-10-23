"use client";

import { useSuggestions, type Suggesting } from "./suggestions";

// TemplateField is a single line that may hold {{variables}}: a header value, a
// query value. Typing {{ lists the variables the step can read.
export function TemplateField({
  value,
  onChange,
  suggest,
  label,
  className,
  invalid,
  describedBy,
}: {
  value: string;
  onChange: (value: string) => void;
  suggest?: Suggesting;
  label: string;
  className?: string;
  invalid?: boolean;
  describedBy?: string;
}) {
  const { props, list } = useSuggestions({ value, onText: onChange, suggest });

  return (
    <div className="relative min-w-0 flex-1">
      <input
        {...props}
        type="text"
        aria-label={label}
        aria-invalid={invalid || undefined}
        aria-describedby={describedBy}
        value={value}
        spellCheck={false}
        autoComplete="off"
        className={className}
      />
      {list}
    </div>
  );
}
