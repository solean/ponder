import "./SettingToggle.css";

/** A bare on/off switch. Give it a visible label with `id`, or an `aria-label`. */
export function SettingToggle({
  id,
  checked,
  onChange,
  disabled,
  "aria-label": ariaLabel,
  "aria-describedby": ariaDescribedBy,
}: {
  id?: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  "aria-label"?: string;
  "aria-describedby"?: string;
}) {
  return (
    <span className="settings-toggle">
      <input
        id={id}
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        aria-label={ariaLabel}
        aria-describedby={ariaDescribedBy}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="settings-toggle-track" aria-hidden="true" />
    </span>
  );
}
