import whiteIcon from "../assets/app-icons/white.png";
import blueIcon from "../assets/app-icons/blue.png";
import charcoalIcon from "../assets/app-icons/charcoal.png";
import type { AppIconAppearance } from "../lib/types";

const options: { value: AppIconAppearance; label: string; image: string }[] = [
  { value: "white", label: "White", image: whiteIcon },
  { value: "blue", label: "Blue", image: blueIcon },
  { value: "charcoal", label: "Charcoal", image: charcoalIcon },
];

export function AppIconPicker({ value, onChange }: {
  value: AppIconAppearance;
  onChange: (value: AppIconAppearance) => void;
}) {
  return (
    <fieldset className="settings-field settings-theme-field app-icon-picker" aria-describedby="app-icon-hint">
      <legend>App icon</legend>
      <div className="app-icon-options">
        {options.map((option) => (
          <label className="app-icon-option" key={option.value}>
            <input type="radio" name="app-icon" value={option.value}
              checked={value === option.value} onChange={() => onChange(option.value)} />
            <span className="app-icon-card">
              <img src={option.image} width="64" height="64" alt="" />
              <span>{option.label}</span>
              <svg className="app-icon-check" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path d="m3 8 3 3 7-7" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </span>
          </label>
        ))}
      </div>
      <small id="app-icon-hint">Changes the Dock icon while Ponder is running. Your choice is saved for the next launch.</small>
    </fieldset>
  );
}
