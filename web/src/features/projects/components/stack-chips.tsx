// StackChips are the short labels of what a project is made of. A project that
// names none gets nothing, not an empty row: no chip is ever drawn without a word
// in it.
export function StackChips({ stack }: { stack: string[] }) {
  if (stack.length === 0) return null;

  return (
    <ul aria-label="Stack" className="flex flex-wrap gap-1.5">
      {stack.map((label) => (
        <li key={label} className="data rounded-md border px-2 py-0.5 text-[11px] text-muted-foreground">
          {label}
        </li>
      ))}
    </ul>
  );
}
