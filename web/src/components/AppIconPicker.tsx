import whiteIcon from "../assets/app-icons/white.png";
import blueIcon from "../assets/app-icons/blue.png";
import charcoalIcon from "../assets/app-icons/charcoal.png";
import type { AppIconAppearance } from "../lib/types";
import { SettingRow } from "./SettingRow";

const options: { value: AppIconAppearance; label: string; image: string }[] = [
  { value: "white", label: "White", image: whiteIcon },
  { value: "blue", label: "Blue", image: blueIcon },
  { value: "charcoal", label: "Charcoal", image: charcoalIcon },
];

/** The Appearance row that picks the Dock icon. */
export function AppIconPicker({ value, onChange }: {
  value: AppIconAppearance;
  onChange: (value: AppIconAppearance) => void;
}) {
  return (
    <SettingRow
      label={<span id="settings-app-icon-label">App icon</span>}
      help={<span id="settings-app-icon-help">Changes the Dock icon while Ponder is running, and is kept for the next launch.</span>}
    >
      <div className="settings-swatches" role="radiogroup" aria-labelledby="settings-app-icon-label"
        aria-describedby="settings-app-icon-help">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={value === option.value}
            className={`settings-swatch settings-swatch--icon${value === option.value ? " is-active" : ""}`}
            onClick={() => onChange(option.value)}
          >
            <img src={option.image} width="36" height="36" alt="" />
            {option.label}
          </button>
        ))}
      </div>
    </SettingRow>
  );
}
