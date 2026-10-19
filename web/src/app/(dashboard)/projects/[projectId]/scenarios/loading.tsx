import { SkeletonHeading, SkeletonPage, SkeletonTable } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the scenarios…">
      <SkeletonHeading />
      <SkeletonTable rows={5} />
    </SkeletonPage>
  );
}
