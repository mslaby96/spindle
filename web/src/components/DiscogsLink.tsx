// Discogs' API terms ask for this exact credit, linked to the record's page, next to any of their data.
export function DiscogsLink({ kind, id }: { kind: "master" | "release"; id: number }) {
  return (
    <a className="discogs" href={`https://www.discogs.com/${kind}/${id}`} target="_blank" rel="noopener">
      Data provided by Discogs
    </a>
  );
}
