// Inline stroke SVG icons translated verbatim from the GoMental design prototype.
// ~1.8-2px stroke, rounded joins, currentColor unless a fixed stroke is required.
import type {CSSProperties} from 'react';

type IconProps = {
  size?: number;
  className?: string;
  style?: CSSProperties;
};

const base = (size: number): {width: number; height: number; viewBox: string; fill: string} => ({
  width: size,
  height: size,
  viewBox: '0 0 24 24',
  fill: 'none',
});

// AppMark is the GoMental brand mark: the rainbow knowledge-graph glyph — a
// magenta hub with colour-ringed nodes on gradient spokes, on a transparent
// background. Kept as inline SVG (crisp at any size, matches build/appicon +
// favicon) rather than an <img>.
export function AppMark({size = 30, style}: {size?: number; style?: CSSProperties}) {
  return (
    <svg width={size} height={size} viewBox="0 0 120 120" fill="none" style={{flex: 'none', ...style}} aria-hidden="true">
      <defs>
        <linearGradient id="gmMarkGrad" x1="10" y1="20" x2="105" y2="100" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#f72585" />
          <stop offset=".5" stopColor="#ffd60a" />
          <stop offset="1" stopColor="#4361ee" />
        </linearGradient>
      </defs>
      <path
        d="M54 56 L30 26 M54 56 L88 30 M54 56 L100 66 M54 56 L70 98 M54 56 L34 94 M54 56 L18 58"
        stroke="url(#gmMarkGrad)"
        strokeWidth="8"
        strokeLinecap="round"
        opacity=".9"
      />
      <circle cx="30" cy="26" r="9" fill="#fff" stroke="#f72585" strokeWidth="5" />
      <circle cx="88" cy="30" r="8.5" fill="#fff" stroke="#ff8500" strokeWidth="5" />
      <circle cx="100" cy="66" r="9" fill="#fff" stroke="#ffce1f" strokeWidth="5" />
      <circle cx="70" cy="98" r="8.5" fill="#fff" stroke="#38b000" strokeWidth="5" />
      <circle cx="34" cy="94" r="9" fill="#fff" stroke="#4cc9f0" strokeWidth="5" />
      <circle cx="18" cy="58" r="7.5" fill="#fff" stroke="#4361ee" strokeWidth="4.5" />
      <circle cx="54" cy="56" r="17" fill="#f72585" />
    </svg>
  );
}

// Wordmark renders "GoMental": "Go" in the theme text colour and "Mental" in the
// magenta→orange→violet→blue rainbow gradient from the logo lockup.
export function Wordmark({className, style}: {className?: string; style?: CSSProperties}) {
  return (
    <span className={className} style={style}>
      <span className="gm-wm-go">Go</span>
      <span className="gm-wm-mental">Mental</span>
    </span>
  );
}

export function SearchIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" className={className} style={style} aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <line x1="21" y1="21" x2="16.65" y2="16.65" />
    </svg>
  );
}

export function PlusIcon({size = 14, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2.6} strokeLinecap="round" className={className} style={style} aria-hidden="true">
      <line x1="12" y1="5" x2="12" y2="19" />
      <line x1="5" y1="12" x2="19" y2="12" />
    </svg>
  );
}

export function ImportIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="M12 3v12" />
      <path d="m7 10 5 5 5-5" />
      <path d="M5 21h14" />
    </svg>
  );
}

export function CloseIcon({size = 14, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2.4} strokeLinecap="round" className={className} style={style} aria-hidden="true">
      <line x1="18" y1="6" x2="6" y2="18" />
      <line x1="6" y1="6" x2="18" y2="18" />
    </svg>
  );
}

export function RefreshIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
      <path d="M21 3v5h-5" />
    </svg>
  );
}

export function GearIcon({size = 16, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.6-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9l-.1-.1A2 2 0 1 1 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3h.1a1.7 1.7 0 0 0 1-1.6V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.6h.1a1.7 1.7 0 0 0 1.9-.3l.1-.1A2 2 0 1 1 20 7l-.1.1a1.7 1.7 0 0 0-.3 1.9v.1a1.7 1.7 0 0 0 1.6 1h.1a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.6 1z" />
    </svg>
  );
}

export function SunIcon({size = 17, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </svg>
  );
}

export function MoonIcon({size = 17, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
    </svg>
  );
}

export function FolderIcon({size = 16, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" className={className} style={{flex: 'none', ...style}} aria-hidden="true">
      <path d="M4 7a2 2 0 0 1 2-2h3.6l1.6 2H18a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2z" />
    </svg>
  );
}

export function FileIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" className={className} style={{flex: 'none', ...style}} aria-hidden="true">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <path d="M14 2v6h6" />
    </svg>
  );
}

export function NoteTabIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={1.9} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <path d="M14 2v6h6" />
      <line x1="8" y1="13" x2="16" y2="13" />
      <line x1="8" y1="17" x2="13" y2="17" />
    </svg>
  );
}

export function GraphTabIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={1.9} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <circle cx="5" cy="6" r="2" />
      <circle cx="19" cy="6" r="2" />
      <circle cx="12" cy="18" r="2" />
      <path d="M6.6 7.5 10.6 16.4M17.4 7.5 13.4 16.4M7 6h10" />
    </svg>
  );
}

export function ChevronIcon({size = 13, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2.4} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="m9 18 6-6-6-6" />
    </svg>
  );
}

export function ClockIcon({size = 13, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7.5v4.7l3 1.8" />
    </svg>
  );
}

export function StarIcon({size = 15, className, style, filled = false}: IconProps & {filled?: boolean}) {
  return (
    <svg
      {...base(size)}
      stroke="currentColor"
      strokeWidth={filled ? 1.6 : 2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      style={style}
      aria-hidden="true"
    >
      <path
        fill={filled ? 'currentColor' : 'none'}
        d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3l-5.6 2.9 1.1-6.2L3 9.6l6.2-.9z"
      />
    </svg>
  );
}

export function CodeIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="m16 18 6-6-6-6" />
      <path d="m8 6-6 6 6 6" />
    </svg>
  );
}

export function SaveIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
      <path d="M17 21v-8H7v8M7 3v5h8" />
    </svg>
  );
}

export function CheckIcon({size = 15, className, style, stroke}: IconProps & {stroke?: string}) {
  return (
    <svg {...base(size)} stroke={stroke || 'currentColor'} strokeWidth={2.6} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="M20 6 9 17l-5-5" />
    </svg>
  );
}

export function LinkIcon({size = 14, className, style, stroke}: IconProps & {stroke?: string}) {
  return (
    <svg {...base(size)} stroke={stroke || 'currentColor'} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={{flex: 'none', ...style}} aria-hidden="true">
      <path d="M9 15 15 9" />
      <path d="M11 6.5 12.9 4.6a4 4 0 0 1 5.7 5.7l-2 1.9" />
      <path d="M13 17.5l-1.9 1.9a4 4 0 0 1-5.7-5.7l2-1.9" />
    </svg>
  );
}

export function EditIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4z" />
    </svg>
  );
}

export function TrashIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <path d="M3 6h18" />
      <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
      <path d="M10 11v6M14 11v6" />
    </svg>
  );
}

export function ImageIcon({size = 15, className, style}: IconProps) {
  return (
    <svg {...base(size)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={style} aria-hidden="true">
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <circle cx="8.5" cy="8.5" r="1.5" />
      <path d="m21 15-5-5L5 21" />
    </svg>
  );
}

export function BulbIcon({size = 18, className, style, stroke}: IconProps & {stroke?: string}) {
  return (
    <svg {...base(size)} stroke={stroke || 'currentColor'} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} style={{flex: 'none', ...style}} aria-hidden="true">
      <path d="M9 18h6M10 21h4M12 2a7 7 0 0 0-4 12.7c.6.5 1 1.3 1 2.1V17h6v-.2c0-.8.4-1.6 1-2.1A7 7 0 0 0 12 2z" />
    </svg>
  );
}
