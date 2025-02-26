package resolver

import "github.com/graphql-go/graphql"

type TypeByCategory struct {
	Group   string
	Version string
	Kind    string
	Scope   string
}

func (s *Service) TypeByCategory(m map[string][]TypeByCategory) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		name, err := getStringArg(p.Args, NameArg, true)
		if err != nil {
			return nil, err
		}

		return m[name], nil
	}
}
