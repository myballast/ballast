import { SkeletonHeading, SkeletonPage, SkeletonTable } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the CI tokens…">
      <SkeletonHeading />
      <SkeletonTable rows={4} />
    </SkeletonPage>
  );
}
