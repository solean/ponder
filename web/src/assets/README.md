# Rank badge assets

`mtga-ranks-constructed-48.png` and `mtga-ranks-limited-48.png` contain the
Magic: The Gathering Arena rank badges at 48px. The sprite sheets were sourced
from [`mtgatool/mtgatool-desktop`](https://github.com/mtgatool/mtgatool-desktop)
and retain the original MTGA artwork.

The individual images under `ranks/` are 48px crops of those sheets for graph
promotion annotations.

# Mana symbol assets

The SVGs under `mana-symbols/` are the mana-representing entries from
[Scryfall's card-symbol API](https://scryfall.com/docs/api/card-symbols). They
are bundled with the frontend so frequently rendered mana costs and color
identities do not depend on requests to `svgs.scryfall.io`.
