/**
 * The colour of somebody's own presence dot, from the status they chose.
 *
 * Only the four statuses every server is created with have a colour of their
 * own; anything an operator added reads as neutral rather than borrowing a
 * meaning it may not have. Appearing offline is drawn hollow — a shape, not
 * only a colour, so it is told apart without seeing colour at all.
 */
export function presenceDotClass(status: string, invisible: boolean): string {
  if (invisible) return "border-2 border-muted-foreground bg-transparent";
  switch (status) {
    case "available":
      return "bg-success";
    case "away":
      return "bg-warning";
    case "busy":
      return "bg-destructive";
    default:
      return "bg-muted-foreground";
  }
}
