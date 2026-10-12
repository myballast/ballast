import { redirect } from "next/navigation";

// The entry point of the application is the project list.
export default function HomePage() {
  redirect("/projects");
}
