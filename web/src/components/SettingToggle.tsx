import type { ReactNode } from "react";

import "./SettingToggle.css";

export function SettingToggle({ checked, onChange, disabled, children }: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  children: ReactNode;
}) {
  return (
    <label className="settings-toggle">
      <input type="checkbox" role="switch" checked={checked} disabled={disabled}
        onChange={(event) => onChange(event.target.checked)} />
      <span className="settings-toggle-track" aria-hidden="true" />
      <span>{children}</span>
    </label>
  );
}
