/** The time zones the dashboard offers, the first being the default. Each keeps one whole-hour offset all year
 * (no summer time), which the hour-of-day charts rely on. */
export const ZONES = [
  { id: "Asia/Bangkok", label: "Bangkok" },
  { id: "Asia/Seoul", label: "Seoul (KST)" },
  { id: "Asia/Tokyo", label: "Japan (JST)" },
  { id: "Asia/Shanghai", label: "China (CST)" },
  { id: "UTC", label: "UTC" },
];

export const DEFAULT_ZONE = ZONES[0].id;

/** v if it is one of ZONES, else undefined. */
export const zoneOf = (v?: string) => (ZONES.some((z) => z.id === v) ? v : undefined);

export const zoneLabel = (id: string) => ZONES.find((z) => z.id === id)?.label ?? id;
