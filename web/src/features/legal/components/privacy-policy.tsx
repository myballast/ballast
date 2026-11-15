import { ContactLink, LegalList, LegalPage, LegalSection } from "./legal-page";

// What Ballast keeps about a person, said plainly. Every statement here is checked
// against the code: what the sign-in stores (app_user), the two cookies, the
// absence of any analytics. A change to one of those changes this page.
export function PrivacyPolicy() {
  return (
    <LegalPage title="Privacy policy" updated="15 November 2026">
      <p>
        Ballast is operated by Youssef Mahersi, who is responsible for the personal data
        described here. For anything about this page or your data, write to <ContactLink />.
      </p>

      <LegalSection title="What Ballast keeps">
        <LegalList>
          <li>
            <strong className="font-medium">Your account.</strong> When you sign in with GitHub or
            Google, Ballast receives and keeps the identifier that provider gives your account, your
            email address and your name. Ballast never sees or stores a password.
          </li>
          <li>
            <strong className="font-medium">What you put in it.</strong> Organizations, projects,
            members and their roles, scenarios, environments and the secrets you give them (encrypted
            at rest, and never shown again once saved), and the results and output of your runs. A
            run&apos;s output can contain what your own target answered.
          </li>
          <li>
            <strong className="font-medium">A journal of actions.</strong> Who launched, cancelled
            or was refused a run, who verified or approved a target, who granted or removed a role,
            and when. It exists to show who did what with load testing; it does not record what you
            read.
          </li>
        </LegalList>
      </LegalSection>

      <LegalSection title="What Ballast does not do">
        <LegalList>
          <li>No analytics, no advertising, no tracking pixels, no third-party scripts that follow you.</li>
          <li>No sale or rental of your data, to anybody.</li>
          <li>No use of your scenarios, targets or results for anything but running your tests.</li>
        </LegalList>
      </LegalSection>

      <LegalSection title="Why">
        <p>
          To provide the service you signed up for: without your account, nobody can sign in, and
          without your scenarios and results there is nothing to run or read. The journal of actions
          is kept for the security of the service and of the servers your tests reach: a load testing
          tool must be able to say who sent load where.
        </p>
      </LegalSection>

      <LegalSection title="Cookies">
        <p>Two, both needed for the site to work, so no consent banner:</p>
        <LegalList>
          <li>a session cookie, which keeps you signed in. It cannot be read by scripts on the page;</li>
          <li>a theme cookie, which remembers whether you chose the light or the dark theme.</li>
        </LegalList>
      </LegalSection>

      <LegalSection title="Where it lives, and who else handles it">
        <p>
          Ballast&apos;s servers and database are in the European Union, in the Netherlands. The
          companies that host them process your data on Ballast&apos;s behalf and for nothing
          else; like any host, they may keep short-lived technical logs, such as IP addresses.
          GitHub and Google handle your sign-in under their own privacy policies.
        </p>
      </LegalSection>

      <LegalSection title="How long">
        <p>
          Your account and everything in it are kept as long as the account exists. Ask for its
          deletion at <ContactLink /> and it is deleted within 30 days. When a person leaves an
          organization, the journal of actions keeps the entries they wrote, no longer linked to
          their account: an action that happened stays recorded.
        </p>
      </LegalSection>

      <LegalSection title="Your rights">
        <p>
          You can ask to see the data Ballast keeps about you, to correct it, to delete it, to receive
          a copy of it, or object to how it is used. Write to <ContactLink />; you will get an answer
          within a month. If you think your data is mishandled, you can also complain to the CNIL,
          the French data protection authority (cnil.fr).
        </p>
      </LegalSection>

      <LegalSection title="Changes">
        <p>
          This page will change when Ballast does. The date at the top says when it last did; a
          change that matters will also be sent to the email address of your account.
        </p>
      </LegalSection>
    </LegalPage>
  );
}
