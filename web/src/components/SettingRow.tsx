import { useId, type ReactNode } from "react";

import { SettingToggle } from "./SettingToggle";

/**
 * One setting: its name and a line of help on the left, its control on the
 * right. Pass `htmlFor` when the control is a single input, so the name
 * labels it.
 */
export function SettingRow({
  label,
  help,
  htmlFor,
  compact,
  children,
}: {
  label: ReactNode;
  help?: ReactNode;
  htmlFor?: string;
  /** A shorter row for lists of simple toggles. */
  compact?: boolean;
  children?: ReactNode;
}) {
  return (
    <div className={`settings-row${compact ? " is-compact" : ""}`}>
      <div className="settings-row-text">
        {htmlFor ? (
          <label className="settings-row-label" htmlFor={htmlFor}>
            {label}
          </label>
        ) : (
          <div className="settings-row-label">{label}</div>
        )}
        {help ? <div className="settings-row-help">{help}</div> : null}
      </div>
      {children ? <div className="settings-row-control">{children}</div> : null}
    </div>
  );
}

/** A setting row whose control is an on/off switch. */
export function ToggleRow({
  label,
  help,
  checked,
  onChange,
  disabled,
  compact,
  before,
}: {
  label: ReactNode;
  help?: ReactNode;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  compact?: boolean;
  /** Extra controls shown before the switch, such as a button. */
  before?: ReactNode;
}) {
  const id = useId();
  const helpId = `${id}-help`;
  return (
    <div className={`settings-row${compact ? " is-compact" : ""}`}>
      <div className="settings-row-text">
        <label className="settings-row-label" htmlFor={id}>
          {label}
        </label>
        {help ? (
          <div className="settings-row-help" id={helpId}>
            {help}
          </div>
        ) : null}
      </div>
      <div className="settings-row-control">
        {before}
        <SettingToggle
          id={id}
          checked={checked}
          onChange={onChange}
          disabled={disabled}
          aria-describedby={help ? helpId : undefined}
        />
      </div>
    </div>
  );
}
