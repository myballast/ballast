"use client";

import { Star } from "lucide-react";
import { useOptimistic, useTransition } from "react";

import { cn } from "@/lib/utils";

import { setFavorite } from "../actions";

// FavoriteButton is the star of a project: pressed, it is the caller's own
// favourite and the project goes first in their list. Nobody else sees it.
//
// The star turns at once and the list is drawn again behind it. Said in words for
// a screen reader — "Remove storefront from favorites" — because a filled star is
// not something everybody sees. It is a toggle, not a status, so its colour is
// the brand's and carries no meaning of its own.
export function FavoriteButton({ projectId, name, favorite }: { projectId: string; name: string; favorite: boolean }) {
  const [shown, flip] = useOptimistic(favorite, (_current, next: boolean) => next);
  const [, startTransition] = useTransition();

  return (
    <button
      type="button"
      aria-pressed={shown}
      aria-label={shown ? `Remove ${name} from favorites` : `Add ${name} to favorites`}
      onClick={() =>
        startTransition(async () => {
          flip(!shown);
          await setFavorite(projectId, !shown);
        })
      }
      className="grid size-7 shrink-0 place-items-center rounded-md hover:bg-muted"
    >
      <Star
        aria-hidden
        className={cn("size-4", shown ? "fill-brand text-brand" : "text-muted-foreground")}
      />
    </button>
  );
}
