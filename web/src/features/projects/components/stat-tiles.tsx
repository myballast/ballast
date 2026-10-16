import { ArrowDown, ArrowRight, ArrowUp } from "lucide-react";

import { Sparkline } from "@/components/sparkline";
import { Card } from "@/components/ui/card";

import type { Tile } from "../stats";

const arrows = { up: ArrowUp, down: ArrowDown, flat: ArrowRight } as const;
const words = { up: "up", down: "down", flat: "unchanged" } as const;

// StatTile is one figure of the project over the last days: its value, how it
// moved from the days before, and the shape of it day by day.
//
// Nothing here is coloured by whether it is good news. A rise in failed runs and
// a rise in total runs are both an arrow and a number; colour is for the status
// of a run and the verdict of a threshold, and a tile that turned green or red
// would be claiming one of them. The direction is said by the arrow and in
// words for a screen reader.
function StatTile({ tile, days }: { tile: Tile; days: number }) {
  const Arrow = tile.direction ? arrows[tile.direction] : null;

  return (
    <Card className="gap-0 overflow-hidden pb-0">
      <div className="flex flex-col gap-2 px-4 pt-4">
        <p className="text-[11px] tracking-wider text-muted-foreground uppercase">{tile.label}</p>
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="num text-2xl">{tile.value}</span>
          {tile.change && (
            <span
              className="num flex items-center gap-1 text-[11px] text-muted-foreground"
              title={`Against the ${days} days before`}
            >
              {Arrow && <Arrow aria-hidden className="size-3" />}
              {tile.direction && tile.direction !== "flat" && <span className="sr-only">{words[tile.direction]} </span>}
              {tile.change}
              <span className="sr-only"> against the {days} days before</span>
            </span>
          )}
        </div>
      </div>
      <div className="mt-2.5">
        <Sparkline series={tile.series} />
      </div>
    </Card>
  );
}

// StatTiles are the five figures of a project's page, over the last days and set
// against the same number before. The figures are added up by the API; this only
// shows them.
export function StatTiles({ tiles, days }: { tiles: Tile[]; days: number }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(min(200px,100%),1fr))] gap-3.5">
      {tiles.map((tile) => (
        <StatTile key={tile.label} tile={tile} days={days} />
      ))}
    </div>
  );
}
