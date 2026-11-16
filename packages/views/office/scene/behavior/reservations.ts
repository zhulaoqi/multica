/**
 * Exclusive station use.
 *
 * Several agents share one coffee machine, printer, and sofa set. Without
 * reservations two routines walk to the same tile and stand inside each other;
 * with them an agent picks another errand (or waits for its turn) instead.
 */
export interface ReservableStation {
  key: string;
  capacity: number;
}

export class Reservations {
  private readonly capacity = new Map<string, number>();
  private readonly owners = new Map<string, Set<string>>();

  constructor(stations: readonly ReservableStation[]) {
    for (const station of stations) {
      this.capacity.set(station.key, Math.max(1, station.capacity));
      this.owners.set(station.key, new Set());
    }
  }

  isFree(key: string): boolean {
    const limit = this.capacity.get(key);
    if (limit === undefined) return false;
    return (this.owners.get(key)?.size ?? 0) < limit;
  }

  /** True when this owner already holds the station. */
  holds(key: string, owner: string): boolean {
    return this.owners.get(key)?.has(owner) ?? false;
  }

  tryAcquire(key: string, owner: string): boolean {
    const owners = this.owners.get(key);
    const limit = this.capacity.get(key);
    if (!owners || limit === undefined) return false;
    if (owners.has(owner)) return true;
    if (owners.size >= limit) return false;
    owners.add(owner);
    return true;
  }

  release(key: string, owner: string) {
    this.owners.get(key)?.delete(owner);
  }

  releaseAll(owner: string) {
    for (const owners of this.owners.values()) owners.delete(owner);
  }
}
