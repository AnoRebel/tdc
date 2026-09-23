/**
 * The directory: profiles somebody chose to publish, and the companies they
 * agreed to be listed by.
 *
 * Every field past a name is optional on a profile somebody else is reading,
 * because the server omits what the viewer may not see — an absent bio means
 * "nothing you may see", and a client must not render it as "nothing written".
 */

/** Who may see a profile, or one section of it. */
export type DirectoryVisibility = "public" | "members" | "hidden";

/** One portfolio link. The identifier is absent on a link not yet saved. */
export interface PortfolioLink {
  id?: string;
  title: string;
  url: string;
  description?: string;
}

/**
 * A profile as its owner writes it, and as the server hands it back to them.
 *
 * The same shape is saved, read back, and found in an export, so an exported
 * profile can be sent straight back to recreate it.
 */
export interface ProfileDocument {
  headline: string;
  bio: string;
  location: string;
  experience: string;
  contactEmail: string;
  contactPhone: string;
  contactUrl: string;
  visibility: DirectoryVisibility;
  detailsVisibility: DirectoryVisibility;
  contactVisibility: DirectoryVisibility;
  portfolio: PortfolioLink[];
}

/** A company a profile's owner has agreed to be listed by. */
export interface CompanyReference {
  slug: string;
  name: string;
  role?: string;
}

/** Somebody's own profile. */
export interface OwnProfile {
  username: string;
  displayName: string;
  address: string;
  profile: ProfileDocument;
  companies: CompanyReference[];
  createdAt: string;
  updatedAt: string;
}

/** A profile as somebody other than its owner sees it. */
export interface ProfileView {
  username: string;
  displayName: string;
  address: string;
  headline?: string;
  bio?: string;
  location?: string;
  experience?: string;
  contactEmail?: string;
  contactPhone?: string;
  contactUrl?: string;
  portfolio?: (PortfolioLink & { id: string })[];
  companies?: CompanyReference[];
  updatedAt: string;
}

/** Somebody a company lists, or has asked to. */
export interface CompanyMember {
  username: string;
  displayName: string;
  address: string;
  role?: string;
  administrator?: boolean;
  /** When they accepted, or when they were asked while still pending. */
  since: string;
}

/** A company profile. */
export interface Company {
  slug: string;
  name: string;
  description?: string;
  location?: string;
  visibility: Exclude<DirectoryVisibility, "hidden">;
  members: CompanyMember[];
  /** Unanswered invitations, shown only to the company's administrators. */
  pending?: CompanyMember[];
  /** Whether the viewer may change the company. */
  administered?: boolean;
}

/** One company somebody belongs to, or has been asked to join. */
export interface CompanyAssociation {
  slug: string;
  name: string;
  role?: string;
  administrator?: boolean;
  pending: boolean;
  invitedAt: string;
  acceptedAt?: string;
}
