type Props = { albumId: number; hasCover: boolean; color?: string; className?: string };

export function Cover({ albumId, hasCover, color, className = "cover" }: Props) {
  if (hasCover) return <img className={className} src={`/api/albums/${albumId}/cover`} alt="" loading="lazy" />;
  return <div className={className} style={{ background: color }} />;
}
