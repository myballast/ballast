import Link from "next/link";

// The mark is the brand's violet, which is what violet is for.
export function Brand() {
  return (
    <Link href="/" className="flex items-center gap-2.5 px-2">
      <span aria-hidden className="grid size-6 place-items-center rounded-md bg-primary">
        <span className="size-2 rounded-[2px] bg-background" />
      </span>
      <span className="text-[15px] font-medium tracking-wide">Ballast</span>
    </Link>
  );
}
