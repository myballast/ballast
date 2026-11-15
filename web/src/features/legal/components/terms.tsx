import Link from "next/link";

import { ContactLink, LegalList, LegalPage, LegalSection } from "./legal-page";

// The terms of the hosted service. The self-hosted software has its own licence
// (FSL-1.1-ALv2) in the repository; nothing here replaces it.
export function Terms() {
  return (
    <LegalPage title="Terms of use" updated="15 November 2026">
      <p>
        These terms apply to Ballast as a hosted service at myballast.pro, operated by Youssef
        Mahersi. By signing in, you accept them. If you run Ballast on your own servers, the
        software&apos;s licence applies instead, and these terms do not.
      </p>

      <LegalSection title="A free beta">
        <p>
          Ballast is in public beta. It is free during the beta. A paid plan will come later: it will
          be announced to the email address of your account before it applies to you, and you will be
          free to stop using the service rather than pay. Self-hosting stays free.
        </p>
        <p>
          A beta changes. Features can be added, changed or removed, and the service can be
          unavailable at times. It is provided as it is, without any guarantee of availability or of
          fitness for a particular purpose.
        </p>
      </LegalSection>

      <LegalSection title="Only load what you own">
        <p>
          Ballast sends real traffic to the servers you name. You may only aim it at servers you own,
          or that their owner has authorised you to test, and Ballast asks you to prove it before any
          run. In particular, you must not:
        </p>
        <LegalList>
          <li>test a server you are not authorised to test, or help someone else do so;</li>
          <li>try to get around the verification of targets, the network restrictions of a run, or the limits on virtual users and duration;</li>
          <li>use Ballast to disrupt, overload or attack any system;</li>
          <li>put in a scenario anything you have no right to use.</li>
        </LegalList>
        <p>
          Refusals and runs are recorded. An account that breaks these rules can be suspended at once,
          and its runs stopped, without notice.
        </p>
      </LegalSection>

      <LegalSection title="Your account and your content">
        <p>
          You sign in with GitHub or Google, and you are responsible for what is done from your
          account and for the people you invite into your organization. What you put in Ballast
          stays yours: you allow Ballast to store and process it only to run the service for you.
          How it is handled is described in the <Link href="/privacy" className="text-brand hover:underline">privacy policy</Link>.
        </p>
      </LegalSection>

      <LegalSection title="Responsibility">
        <p>
          You are responsible for the tests you run and for their effects on your own systems. As far
          as the law allows, Ballast is not liable for indirect losses, for lost data or revenue, or
          for an outage of the service during the beta. Nothing in these terms limits a right that the
          law gives you and that a contract cannot take away.
        </p>
      </LegalSection>

      <LegalSection title="Ending">
        <p>
          You can stop using Ballast at any time, and ask for your account to be deleted at{" "}
          <ContactLink />. Ballast can end the beta or the service; you would be told by email at least
          30 days before.
        </p>
      </LegalSection>

      <LegalSection title="Changes and law">
        <p>
          These terms can change; a change that matters is sent to the email address of your account
          before it applies. They are governed by French law. If you use Ballast as a consumer, you
          keep the protections of the law of the country where you live.
        </p>
        <p>
          Questions: <ContactLink />.
        </p>
      </LegalSection>
    </LegalPage>
  );
}
