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
  skills: ProfileSkill[];
  /** Whether they are looking for work, true until `availableUntil`. */
  openToWork: boolean;
  /** The kind of engagement sought. */
  availabilityNote: string;
  /** When the statement lapses, as an RFC 3339 instant. */
  availableUntil?: string;
  /**
   * Whether the profile takes enquiries: null until the owner chooses, when
   * it follows the visibility — a public profile takes them.
   */
  acceptsEnquiries: boolean | null;
}

/** Where an enquiry stands. */
export type EnquiryStatus = "pending" | "accepted" | "declined" | "withdrawn";

/** One enquiry, from the point of view of one of its two people. */
export interface Enquiry {
  id: string;
  subject: string;
  body: string;
  status: EnquiryStatus;
  /** The private conversation it opened. */
  roomId?: string;
  /** Whether the viewer received it, rather than sent it. */
  received: boolean;
  /** The other person's username, and their name. */
  with: string;
  withName: string;
  createdAt: string;
  respondedAt?: string;
  /** The engagement it became, once taken up. */
  engagementId?: string;
}

/** Where an engagement stands. */
export type EngagementState =
  | "discussion"
  | "proposal"
  | "negotiation"
  | "agreement"
  | "active"
  | "completion_pending"
  | "completed"
  | "withdrawn";

/** One recorded change of an engagement's state. */
export interface EngagementTransition {
  from: EngagementState | "enquiry";
  to: EngagementState;
  actor: string;
  note?: string;
  at: string;
}

/** An engagement, as one of its two parties sees it. */
export interface Engagement {
  id: string;
  title: string;
  state: EngagementState;
  /** Each side, with what to call them: a display name, or the username without one. */
  client: { username: string; displayName: string; address: string };
  developer: { username: string; displayName: string; address: string };
  /** Which party the viewer is. */
  you: "client" | "developer";
  roomId?: string;
  enquiryId?: string;
  /** Whether either party may still withdraw: there is no agreement yet. */
  withdrawable: boolean;
  completionRequestedBy?: string;
  completionRequestedAt?: string;
  transitions?: EngagementTransition[];
  /**
   * The project the agreed work is tracked in — present only when the viewer
   * can see that project, so a private one is never named to somebody outside
   * it.
   */
  project?: { key: string; name: string };
  createdAt: string;
  updatedAt: string;
}

/** An engagement as a project shows it to one of its parties: no terms. */
export interface LinkedEngagement {
  id: string;
  title: string;
  state: EngagementState;
}

/** Whether a skill is in the vocabulary or waiting for review. */
export type SkillStatus = "active" | "proposed";

/** One entry in the skill vocabulary, or one waiting for it. */
export interface Skill {
  id: string;
  name: string;
  description?: string;
  status: SkillStatus;
}

/**
 * A skill on somebody's profile, with what they say about it.
 *
 * Sent by `id`, or by a `name` the vocabulary knows. `status` is only ever
 * read, and only its owner sees "proposed".
 */
export interface ProfileSkill {
  id?: string;
  name: string;
  status?: SkillStatus;
  description: string;
}

/** A proposal in the review queue. */
export interface QueuedSkill {
  id: string;
  name: string;
  description?: string;
  proposedBy?: string;
  /** How many profiles carry it already. */
  profiles: number;
  createdAt: string;
}

/** One profile in a page of search results. */
export interface DirectoryHit {
  username: string;
  displayName: string;
  address: string;
  headline?: string;
  location?: string;
  available?: boolean;
  availabilityNote?: string;
  skills?: string[];
  updatedAt: string;
}

/** One page of a directory search. The page size is the server's. */
export interface DirectoryResults {
  profiles: DirectoryHit[];
  page: number;
  pageSize: number;
  more: boolean;
}

/** What a directory search can be narrowed by. Every one is optional. */
export interface DirectoryFilters {
  q?: string;
  skill?: string;
  available?: boolean;
  location?: string;
  company?: string;
  page?: number;
}

/**
 * That a moderator verified one claim: who, and when.
 *
 * `note` — what they checked — reaches only the profile's owner, since it may
 * mention something the owner hides from everybody else.
 */
export interface Verified {
  by: string;
  at: string;
  note?: string;
}

/** A company a profile's owner has agreed to be listed by. */
export interface CompanyReference {
  slug: string;
  name: string;
  role?: string;
  verified?: Verified;
}

/** What sort of thing a verifiable claim is. */
export type ClaimKind = "skill" | "portfolio" | "identity" | "company";

/** One of the owner's own verifications. */
export interface OwnVerification extends Verified {
  kind: ClaimKind;
  claimId: string;
}

/** A verifiable claim on a profile, as a verifier reads it. */
export interface Claim {
  kind: ClaimKind;
  id: string;
  label: string;
  content: Record<string, string>;
  /** The standing verification, whose `id` is what revoking takes. */
  verification?: Verified & { id: string };
}

/** Every verifiable claim on one profile. */
export interface ProfileClaims {
  username: string;
  displayName: string;
  claims: Claim[];
}

/** Somebody's own profile. */
export interface OwnProfile {
  username: string;
  displayName: string;
  address: string;
  profile: ProfileDocument;
  companies: CompanyReference[];
  verifications: OwnVerification[];
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
  portfolio?: (PortfolioLink & { id: string; verified?: Verified })[];
  companies?: CompanyReference[];
  skills?: { id: string; name: string; description?: string; verified?: Verified }[];
  /** Present when a moderator verified the person is who the profile says. */
  identityVerified?: Verified;
  /** Whether the viewer could send an enquiry now. */
  acceptsEnquiries: boolean;
  /** Present only while the owner's availability is current. */
  available?: boolean;
  availabilityNote?: string;
  availableUntil?: string;
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

/** One deliverable in a proposal, priced in whole minor units of its currency. */
export interface LineItem {
  description: string;
  amountMinor: number;
}

/** A proposal as somebody writes it. */
export interface ProposalDraft {
  scope: string;
  timeline: string;
  lineItems: LineItem[];
  /** An ISO 4217 code, such as TZS. */
  currency: string;
}

/** One sent version of a proposal. Never changed once sent. */
export interface ProposalVersion extends ProposalDraft {
  id: string;
  version: number;
  /** The revision it was sent in; absent before the first agreement. */
  revision?: number;
  sentBy: string;
  totalMinor: number;
  sentAt: string;
}

/** How somebody answered a version. */
export type ProposalResponseKind = "accept" | "decline" | "request_changes";

/** One answer to one version. */
export interface ProposalResponse {
  version: number;
  responder: string;
  kind: ProposalResponseKind;
  /** What a change request is about: "scope", "timeline", "currency" or "item:N". */
  part?: string;
  comment?: string;
  at: string;
}

/** The terms both parties accepted, and when each did. */
export interface Agreement {
  version: number;
  /** The revision it came from; 0 for the first agreement. */
  revision: number;
  clientAcceptedAt: string;
  developerAcceptedAt: string;
  terms: ProposalVersion;
  recordedAt: string;
}

/** How a revision of an agreement ended, or that it has not. */
export type RevisionOutcome = "open" | "accepted" | "declined" | "withdrawn";

/** One revision of an agreement in force: who proposed it, why, and how it went. */
export interface AgreementRevision {
  number: number;
  openedBy: string;
  reason: string;
  openedAt: string;
  outcome: RevisionOutcome;
  outcomeBy?: string;
  outcomeNote?: string;
  outcomeAt?: string;
}

/** Everything proposed and answered on one engagement. */
export interface Negotiation {
  versions: ProposalVersion[];
  responses: ProposalResponse[];
  /** The agreement in force: the latest one both accepted. */
  agreement?: Agreement;
  /** Every agreement recorded, the first one first. */
  agreements: Agreement[];
  /** Every revision of the agreement, the first one first. */
  revisions: AgreementRevision[];
  /** What the server is and is not, shown wherever an agreement is. */
  disclosure: string;
}
