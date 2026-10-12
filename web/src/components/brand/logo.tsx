import { cn } from "@/lib/utils";

// The Ballast mark, inline so that it takes the colour of its text: give it
// `text-brand` for the brand's violet, or let it inherit. The same drawing is
// served as public/ballast.svg and, in violet, as the favicon (app/icon.svg).
//
// Decorative by default. Pass a `title` when the mark stands alone with no
// word next to it, so that it is announced.
export function Logo({ className, title }: { className?: string; title?: string }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 620 619"
      fill="currentColor"
      role={title ? "img" : undefined}
      aria-hidden={title ? undefined : true}
      className={cn("size-6 shrink-0", className)}
    >
      {title && <title>{title}</title>}
      <rect x="214.592" width="188.614" height="75.3075" rx="3"/>
      <path stroke="currentColor" d="M145.844 109.386L90.2549 237.279L306.279 157.545C308.988 156.545 311.968 156.555 314.67 157.575L420.592 197.533L530.705 239.072L473.016 109.363C472.535 108.28 471.46 107.582 470.275 107.582H148.595C147.401 107.582 146.32 108.291 145.844 109.386Z"/>
      <path stroke="currentColor" d="M292.04 199.625L0.689453 300.632L60.4618 427.403C60.9594 428.458 62.0239 429.13 63.1908 429.124L289.055 427.954C290.706 427.945 292.04 426.604 292.04 424.954V199.625Z"/>
      <path stroke="currentColor" d="M327.339 199.625L618.689 300.632L558.918 427.991C558.42 429.051 557.35 429.725 556.179 429.716L330.316 427.961C328.668 427.948 327.339 426.609 327.339 424.961V199.625Z"/>
      <path stroke="currentColor" d="M150.011 616.353L74.4492 466.787H544.403L469.891 616.338C469.384 617.356 468.344 618 467.206 618H152.689C151.557 618 150.521 617.363 150.011 616.353Z"/>
    </svg>
  );
}
