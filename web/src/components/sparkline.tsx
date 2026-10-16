const width = 220;
const height = 44;
// Room above and below the line, so that a peak does not touch the edge.
const margin = 4;

// segments cuts a series into runs of consecutive days that have a value, as
// [x, y] points in the drawing's own units. A day without a value breaks the
// line: joining the days either side of it would draw a trend through a day
// that had no data.
export function segments(series: (number | null)[]): [number, number][][] {
  const known = series.filter((v): v is number => v !== null);
  if (known.length === 0) return [];

  const low = Math.min(...known);
  const high = Math.max(...known);
  const span = high - low;

  const out: [number, number][][] = [];
  let current: [number, number][] = [];
  series.forEach((value, i) => {
    if (value === null) {
      if (current.length > 0) out.push(current);
      current = [];
      return;
    }
    const x = series.length === 1 ? width / 2 : (i / (series.length - 1)) * width;
    // A flat series sits in the middle rather than on an edge.
    const y = span === 0 ? height / 2 : margin + (1 - (value - low) / span) * (height - 2 * margin);
    current.push([x, y]);
  });
  if (current.length > 0) out.push(current);
  return out;
}

const point = ([x, y]: [number, number]) => `${x.toFixed(1)} ${y.toFixed(1)}`;

// Sparkline is a small curve of a series, drawn in the brand's violet: it is
// the shape of the figure over the days and carries no reading of its own, so
// it is hidden from assistive technology and the tile's value says the rest. A
// plain SVG, no chart library.
export function Sparkline({ series }: { series: (number | null)[] }) {
  const runs = segments(series);
  if (runs.length === 0) return <div aria-hidden className="h-11" />;

  return (
    <svg
      aria-hidden
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      className="block h-11 w-full overflow-visible"
    >
      {runs.map((run, i) => (
        <g key={i}>
          {run.length > 1 && (
            <path
              d={`M${run.map(point).join(" L")} L${run[run.length - 1][0].toFixed(1)} ${height} L${run[0][0].toFixed(1)} ${height} Z`}
              className="fill-brand/10"
            />
          )}
          {/* A lone day is a dot: a line of one point is no line. */}
          <polyline
            points={(run.length > 1 ? run : [run[0], [run[0][0] + 0.01, run[0][1]]]).map(([x, y]) => `${x.toFixed(2)},${y.toFixed(2)}`).join(" ")}
            fill="none"
            strokeWidth={run.length > 1 ? 1.4 : 4}
            strokeLinecap="round"
            strokeLinejoin="round"
            vectorEffect="non-scaling-stroke"
            className="stroke-brand"
          />
        </g>
      ))}
    </svg>
  );
}
