type Props = {
  value?: number;
  onChange: (halfStars: number | undefined) => void;
  highlight?: boolean;
};

// Ratings are stored in half stars: 1 is half a star, 10 is five stars.
export function RatingStars({ value, onChange, highlight }: Props) {
  return (
    <div className="stars" role="radiogroup" aria-label="Rating">
      {highlight && <span className="muted hint">Rate this album</span>}
      {[1, 2, 3, 4, 5].map((star) => {
        const fill = value === undefined ? 0 : Math.min(Math.max(value - (star - 1) * 2, 0), 2);
        return (
          <span key={star} className="star" data-fill={fill}>
            {[star * 2 - 1, star * 2].map((v) => (
              <button
                key={v}
                type="button"
                role="radio"
                aria-checked={value === v}
                aria-label={`${v / 2} stars`}
                onClick={() => onChange(value === v ? undefined : v)}
              />
            ))}
          </span>
        );
      })}
    </div>
  );
}

export function formatRating(halfStars: number) {
  return "★".repeat(Math.floor(halfStars / 2)) + (halfStars % 2 ? "½" : "");
}
