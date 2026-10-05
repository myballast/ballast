export default function DashboardLayout({ children }: LayoutProps<"/">) {
  return <main className="mx-auto w-full max-w-6xl px-8 py-7">{children}</main>;
}
