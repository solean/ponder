import { APP_NAME } from "../lib/branding";
import { ExternalLink } from "./ExternalLink";
import { SettingRow } from "./SettingRow";

const REPO_URL = "https://github.com/solean/ponder";
const NEW_ISSUE_URL = `${REPO_URL}/issues/new`;
const LICENSE_URL = `${REPO_URL}/blob/master/LICENSE`;
const NOTICES_URL = `${REPO_URL}/blob/master/THIRD_PARTY_NOTICES.md`;
const FAN_CONTENT_POLICY_URL = "https://company.wizards.com/en/legal/fancontentpolicy";
const SCRYFALL_URL = "https://scryfall.com";
const SEVENTEEN_LANDS_URL = "https://www.17lands.com";

/** The Settings page's last section: version, license, credits, and the Fan Content disclaimer. */
export function AboutSection({ id, version }: { id: string; version?: string }) {
  const titleId = `${id}-title`;
  return (
    <section id={id} className="settings-section" aria-labelledby={titleId}>
      <div className="settings-section-head">
        <h3 id={titleId}>About</h3>
      </div>
      <div className="panel settings-card">
        <SettingRow label={APP_NAME} help={<span className="settings-about-version">Version {version || "unknown"}</span>}>
          <ExternalLink href={REPO_URL}>GitHub ↗</ExternalLink>
          <ExternalLink href={NEW_ISSUE_URL}>Report an issue ↗</ExternalLink>
        </SettingRow>
        <SettingRow
          label="License"
          help="GNU General Public License v3.0 or later. Bundled fonts and Wizards of the Coast material keep their own terms."
        >
          <ExternalLink href={LICENSE_URL}>License ↗</ExternalLink>
          <ExternalLink href={NOTICES_URL}>Third-party notices ↗</ExternalLink>
        </SettingRow>
        <SettingRow label="Card data and images" help="Provided by Scryfall.">
          <ExternalLink href={SCRYFALL_URL}>Scryfall ↗</ExternalLink>
        </SettingRow>
        <SettingRow label="Draft ratings" help="Provided by 17Lands.">
          <ExternalLink href={SEVENTEEN_LANDS_URL}>17Lands ↗</ExternalLink>
        </SettingRow>
      </div>
      <p className="settings-footnote">
        {APP_NAME} is unofficial Fan Content permitted under the Wizards of the Coast{" "}
        <ExternalLink href={FAN_CONTENT_POLICY_URL} className="settings-footnote-link">
          Fan Content Policy
        </ExternalLink>
        . It is not approved or endorsed by Wizards. Portions of the materials used are property of Wizards of the
        Coast. ©Wizards of the Coast LLC.
      </p>
    </section>
  );
}
