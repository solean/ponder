import type { KeyboardEvent } from "react";

export type SectionTabItem<T extends string> = {
  readonly id: T;
  readonly label: string;
};

export function sectionTabID(baseId: string, sectionId: string) {
  return `${baseId}-tab-${sectionId}`;
}

export function sectionPanelID(baseId: string, sectionId: string) {
  return `${baseId}-panel-${sectionId}`;
}

/**
 * Page-level section navigation: underline tabs that own the content below them.
 * Deliberately distinct from `.tabs` (segmented) which is reserved for app nav
 * and panel-scoped switchers, so a page can nest all three without ambiguity.
 */
export function SectionTabs<T extends string>({
  sections,
  activeSection,
  baseId,
  label,
  onSelect,
}: {
  sections: readonly SectionTabItem<T>[];
  activeSection: T;
  baseId: string;
  label: string;
  onSelect: (sectionId: T) => void;
}) {
  function handleKeyDown(event: KeyboardEvent<HTMLButtonElement>, sectionId: T) {
    const currentIndex = sections.findIndex((candidate) => candidate.id === sectionId);
    if (currentIndex === -1) return;

    let nextIndex: number;
    switch (event.key) {
      case "ArrowLeft":
      case "ArrowUp":
        nextIndex = (currentIndex + sections.length - 1) % sections.length;
        break;
      case "ArrowRight":
      case "ArrowDown":
        nextIndex = (currentIndex + 1) % sections.length;
        break;
      case "Home":
        nextIndex = 0;
        break;
      case "End":
        nextIndex = sections.length - 1;
        break;
      default:
        return;
    }

    event.preventDefault();
    const next = sections[nextIndex];
    onSelect(next.id);
    document.getElementById(sectionTabID(baseId, next.id))?.focus();
  }

  return (
    <div className="section-tabs" role="tablist" aria-label={label}>
      {sections.map((section) => (
        <button
          key={section.id}
          type="button"
          id={sectionTabID(baseId, section.id)}
          role="tab"
          aria-selected={activeSection === section.id}
          aria-controls={sectionPanelID(baseId, section.id)}
          tabIndex={activeSection === section.id ? 0 : -1}
          className={`section-tab ${activeSection === section.id ? "is-active" : ""}`}
          onClick={() => onSelect(section.id)}
          onKeyDown={(event) => handleKeyDown(event, section.id)}
        >
          {section.label}
        </button>
      ))}
    </div>
  );
}
