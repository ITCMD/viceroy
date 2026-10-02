import { createLucideIcon } from "lucide-react";

/** Lucide has no shooting star: its star, shrunk into the top-right corner, with three trails. */
export const ShootingStar = createLucideIcon({
  name: "shooting-star",
  size: 24,
  node: [
    [
      "path",
      {
        d: "M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z",
        transform: "translate(8.2 .6) scale(.66)",
        vectorEffect: "non-scaling-stroke",
        key: "star",
      },
    ],
    ["path", { d: "M10 14 3 21", key: "t1" }],
    ["path", { d: "M7.5 10.5 3 15", key: "t2" }],
    ["path", { d: "M13.5 17.5 9 22", key: "t3" }],
  ],
});
