import { safeHref, type Block, type Inline } from "../markdown";

// Draws a page of the documentation from its tree. Every string is written by React as
// text, and a link is drawn only where safeHref allows it: anything else is its own text.
export function MarkdownView({ blocks }: { blocks: Block[] }) {
  return (
    <div className="flex flex-col gap-4 text-sm leading-relaxed">
      {blocks.map((block, i) => (
        <BlockView key={i} block={block} />
      ))}
    </div>
  );
}

function BlockView({ block }: { block: Block }) {
  switch (block.kind) {
    case "heading":
      // The title of the page is the frame's; a second level starts a section.
      return block.level === 1 ? null : (
        <h2 id={block.id} className="mt-6 scroll-mt-6 text-base font-medium">
          <Inlines inlines={block.children} />
        </h2>
      );
    case "paragraph":
      return (
        <p>
          <Inlines inlines={block.children} />
        </p>
      );
    case "code":
      return (
        <pre className="overflow-x-auto rounded-md border bg-muted p-4 font-mono text-xs leading-relaxed">
          <code>{block.text}</code>
        </pre>
      );
    case "list": {
      const List = block.ordered ? "ol" : "ul";
      return (
        <List className={`flex flex-col gap-1.5 pl-5 ${block.ordered ? "list-decimal" : "list-disc"}`}>
          {block.items.map((item, i) => (
            <li key={i}>
              <Inlines inlines={item} />
            </li>
          ))}
        </List>
      );
    }
    case "table":
      return (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full border-collapse text-left text-sm">
            <thead className="border-b bg-muted text-muted-foreground">
              <tr>
                {block.header.map((cell, i) => (
                  <th key={i} className="px-3 py-2 font-medium">
                    <Inlines inlines={cell} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, r) => (
                <tr key={r} className="border-b last:border-b-0 align-top">
                  {row.map((cell, i) => (
                    <td key={i} className="px-3 py-2">
                      <Inlines inlines={cell} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
  }
}

function Inlines({ inlines }: { inlines: Inline[] }) {
  return (
    <>
      {inlines.map((inline, i) => {
        switch (inline.kind) {
          case "text":
            return inline.text;
          case "code":
            return (
              <code key={i} className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]">
                {inline.text}
              </code>
            );
          case "bold":
            return (
              <strong key={i} className="font-medium text-foreground">
                <Inlines inlines={inline.children} />
              </strong>
            );
          case "link": {
            const href = safeHref(inline.href);
            if (href === null) return <Inlines key={i} inlines={inline.children} />;
            const inPage = href.startsWith("#");
            return (
              <a
                key={i}
                href={href}
                className="text-brand hover:underline"
                {...(inPage ? {} : { target: "_blank", rel: "noopener noreferrer" })}
              >
                <Inlines inlines={inline.children} />
              </a>
            );
          }
        }
      })}
    </>
  );
}
