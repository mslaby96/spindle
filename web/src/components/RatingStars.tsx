type Props = {
  value?: number;
  onChange: (halfStars: number | undefined) => void;
  highlight?: boolean;
};

const starPath = "M12 2.4l2.95 6.2 6.75.85-4.98 4.67 1.3 6.73L12 17.55l-6.02 3.3 1.3-6.73L2.3 9.45l6.75-.85z";

function Star({ fill }: { fill: number }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="star-svg">
      <path d={starPath} className="star-empty" />
      {fill > 0 && <path d={starPath} className="star-full" style={fill === 1 ? { clipPath: "inset(0 50% 0 0)" } : undefined} />}
    </svg>
  );
}

const fillOf = (value: number, star: number) => Math.min(Math.max(value - (star - 1) * 2, 0), 2);

// Ratings are stored in half stars: 1 is half a star, 10 is five stars.
export function RatingStars({ value, onChange, highlight }: Props) {
  return (
    <div className="stars" role="radiogroup" aria-label="Rating">
      {highlight && <span className="muted hint">Rate this album</span>}
      {[1, 2, 3, 4, 5].map((star) => (
        <span key={star} className="star">
          <Star fill={value === undefined ? 0 : fillOf(value, star)} />
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
      ))}
    </div>
  );
}

export function Rating({ value }: { value: number }) {
  return (
    <span className="rating" role="img" aria-label={`${value / 2} stars`}>
      {Array.from({ length: Math.ceil(value / 2) }, (_, i) => (
        <Star key={i} fill={fillOf(value, i + 1)} />
      ))}
    </span>
  );
}
