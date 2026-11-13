import { redirect } from "next/navigation";

// This fork runs without billing, so a Stripe return URL (kept from the
// upstream product) must not land on a billing surface. Send the browser back
// to the app root instead of rendering the checkout-return page.
export default async function BillingReturnRoute() {
  redirect("/");
}
