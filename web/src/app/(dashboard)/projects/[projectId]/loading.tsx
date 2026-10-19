import { SkeletonHeading, SkeletonPage, SkeletonTable, SkeletonTiles } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the project…">
      <SkeletonHeading />
      <SkeletonTiles count={5} />
      <SkeletonTable rows={4} />
    </SkeletonPage>
  );
}
