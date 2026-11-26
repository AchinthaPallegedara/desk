"use client";

import { useParams } from "next/navigation";
import { DocumentPage } from "@/components/document-page";

export default function Page() {
  const { id } = useParams<{ id: string }>();
  return <DocumentPage kind="invoice" id={id} />;
}
