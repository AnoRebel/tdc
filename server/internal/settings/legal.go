package settings

/*
Where this server's own policies are published.

The repository carries a privacy policy and terms of use as templates, and
they are only templates: what a server keeps, for how long, and who answers
for it are the operator's to say, in words the operator is accountable for.
These settings are where the operator says where those words are, and every
client links to them from its help centre.

Links rather than text kept here. An operator already publishes these
somewhere — a website, a wiki, a repository — and a policy edited in two places
is a policy that disagrees with itself in the one place nobody checked. A link
also keeps a long legal document out of a response every client reads on every
start.

Empty by default, and empty means the clients say nothing rather than linking
to the templates: a template is not a promise the operator has made, and
presenting one as this server's policy would be worse than presenting none.
*/
const (
	// LegalPrivacyPolicyURL is a link to this server's privacy policy.
	LegalPrivacyPolicyURL Key = "legal.privacy_policy_url"
	// LegalTermsURL is a link to this server's terms of use.
	LegalTermsURL Key = "legal.terms_url"
)

func init() {
	define(map[Key]definition{
		LegalPrivacyPolicyURL: {
			fallback:    "",
			description: "A link to this server's privacy policy, shown in every client's help centre. Empty shows none.",
			validate:    httpURL,
		},
		LegalTermsURL: {
			fallback:    "",
			description: "A link to this server's terms of use, shown in every client's help centre. Empty shows none.",
			validate:    httpURL,
		},
	})
}
