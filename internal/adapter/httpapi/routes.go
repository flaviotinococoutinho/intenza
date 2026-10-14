package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/reputation"
	"github.com/flaviotinococoutinho/intenza/internal/domain/schema"
	"github.com/flaviotinococoutinho/intenza/internal/domain/selection"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/standards"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var bearer = []map[string][]string{{"bearer": {}}}

type idPath struct {
	ID string `path:"id" doc:"Identificador"`
}

type body[T any] struct {
	Body T
}

// --- taxonomy and standards ------------------------------------------------

type categoryView struct {
	ID       string                `json:"id"`
	Label    string                `json:"label"`
	MCC      string                `json:"mcc"`
	MCCName  string                `json:"mcc_name"`
	Wave     int                   `json:"wave"`
	Density  taxonomy.Density      `json:"density"`
	Leaf     bool                  `json:"leaf"`
	Requires []taxonomy.Credential `json:"requires"`
	Fields   []schema.Field        `json:"fields,omitempty"`
}

func view(c taxonomy.Category, withFields bool) categoryView {
	name, ok := c.MCC().DescriptionPT()
	if !ok {
		name = c.MCC().Description()
	}
	v := categoryView{
		ID: c.ID(), Label: c.Label(), MCC: c.MCC().String(), MCCName: name, Wave: c.Wave(),
		Density: c.Density(), Leaf: c.Leaf(), Requires: c.Requires(),
	}
	if withFields && c.Leaf() {
		v.Fields = c.Schema().Fields
	}
	return v
}

// --- intents ---------------------------------------------------------------

type publishInput struct {
	IdempotencyKey string `header:"Idempotency-Key" doc:"Repetir com a mesma chave devolve a mesma intenção"`
	Body           struct {
		CategoryID  string         `json:"category_id" example:"collectibles.tcg"`
		Title       string         `json:"title" minLength:"5" maxLength:"120"`
		Constraints map[string]any `json:"constraints" doc:"Restrições sobre os atributos da categoria"`
		DeliveryCEP string         `json:"delivery_cep" example:"01310-100"`
		Window      string         `json:"window" example:"P2D" doc:"Janela de propostas em ISO 8601"`
	}
}

type intentOutput struct {
	Location string `header:"Location"`
	Status   int
	Body     app.IntentView
}

type submitBody struct {
	Attributes   map[string]any `json:"attributes"`
	Price        money.Money    `json:"price" doc:"Preço total (item + frete), selado até o fechamento"`
	LeadTimeDays int            `json:"lead_time_days" minimum:"1" maximum:"60"`
	ValidUntil   time.Time      `json:"valid_until" doc:"Precisa cobrir a janela de seleção"`
}

type submitInput struct {
	ID   string `path:"id"`
	Body submitBody
}

type proposalPath struct {
	ID         string `path:"id"`
	ProposalID string `path:"pid"`
}

type reviseInput struct {
	proposalPath
	Body submitBody
}

type rankingInput struct {
	ID         string  `path:"id"`
	Method     string  `query:"method" enum:"weighted-sum,topsis" default:"weighted-sum"`
	Price      float64 `query:"w_price" default:"0.4"`
	Reputation float64 `query:"w_reputation" default:"0.25"`
	Adherence  float64 `query:"w_adherence" default:"0.25"`
	LeadTime   float64 `query:"w_lead_time" default:"0.1"`
}

type awardInput struct {
	ID   string `path:"id"`
	Body struct {
		ProposalID string `json:"proposal_id"`
	}
}

// --- agreements ------------------------------------------------------------

type shipInput struct {
	ID   string `path:"id"`
	Body struct {
		Tracking string `json:"tracking" minLength:"4"`
	}
}

type disputeInput struct {
	ID   string `path:"id"`
	Body struct {
		Claims []agreement.ClaimedField `json:"claims" minItems:"1"`
	}
}

type verdictInput struct {
	ID   string `path:"id"`
	Body struct {
		Confirmed []string `json:"confirmed" doc:"Campos cuja discrepância a inspeção confirmou"`
	}
}

type rateInput struct {
	ID   string `path:"id"`
	Body struct {
		Stars int `json:"stars" minimum:"1" maximum:"5"`
	}
}

func (s *server) routes(api huma.API) {
	limitRead := func(ctx context.Context, scope string) error {
		who, _ := requireParty(ctx)
		if who == "" {
			who = "anon"
		}
		return s.limit(ctx, scope, who, 20, 40)
	}

	huma.Register(api, huma.Operation{
		OperationID: "taxonomy", Method: http.MethodGet, Path: "/v1/taxonomy", Tags: []string{"catálogo"},
		Summary: "Árvore de categorias curada",
	},
		func(ctx context.Context, _ *struct{}) (*body[[]categoryView], error) {
			var out []categoryView
			for c := range s.d.Market.Tree.All() {
				out = append(out, view(c, false))
			}
			return &body[[]categoryView]{out}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "category", Method: http.MethodGet, Path: "/v1/taxonomy/{id}", Tags: []string{"catálogo"},
		Summary: "Categoria com o schema de atributos",
	},
		func(ctx context.Context, in *idPath) (*body[categoryView], error) {
			c, err := s.d.Market.Tree.Lookup(in.ID)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[categoryView]{view(c, true)}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "standards", Method: http.MethodGet, Path: "/v1/standards", Tags: []string{"catálogo"},
		Summary: "Normas que fundamentam o domínio, com a cadeia epistêmica de cada uma",
	},
		func(ctx context.Context, _ *struct{}) (*body[[]meta.Standard], error) {
			return &body[[]meta.Standard]{standards.All()}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "publish-intent", Method: http.MethodPost, Path: "/v1/intents", Tags: []string{"intenções"},
		Summary: "Publica uma intenção de compra", Security: bearer, DefaultStatus: http.StatusCreated,
	},
		func(ctx context.Context, in *publishInput) (*intentOutput, error) {
			buyer, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.limit(ctx, "intents", buyer, 2, 5); err != nil {
				return nil, err
			}
			window, err := iso8601.Parse(in.Body.Window)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("window: " + err.Error())
			}
			it, created, err := s.d.Market.PublishIntent(ctx, buyer, in.IdempotencyKey, intent.PublishCommand{
				CategoryID: in.Body.CategoryID, Title: in.Body.Title, Constraints: in.Body.Constraints,
				DeliveryCEP: in.Body.DeliveryCEP, Window: window,
			})
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			status := http.StatusCreated
			if !created {
				status = http.StatusOK
			}
			return &intentOutput{
				Location: "/v1/intents/" + it.ID, Status: status,
				Body: app.IntentView{Intent: it, EffectiveStatus: it.EffectiveStatus(s.d.Market.Clock.Now())},
			}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "get-intent", Method: http.MethodGet, Path: "/v1/intents/{id}", Tags: []string{"intenções"}},
		func(ctx context.Context, in *idPath) (*body[app.IntentView], error) {
			if err := limitRead(ctx, "read"); err != nil {
				return nil, err
			}
			v, err := s.d.Market.Intent(ctx, in.ID)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[app.IntentView]{v}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "list-intents", Method: http.MethodGet, Path: "/v1/intents", Tags: []string{"intenções"},
		Summary: "Intenções abertas de uma subárvore de categorias",
	},
		func(ctx context.Context, in *struct {
			Category string `query:"category" example:"collectibles"`
			Limit    int    `query:"limit" default:"50" maximum:"100"`
		},
		) (*body[[]intent.Intent], error) {
			if err := limitRead(ctx, "read"); err != nil {
				return nil, err
			}
			list, err := s.d.Market.OpenIntents(ctx, in.Category, in.Limit)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[[]intent.Intent]{list}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "cancel-intent", Method: http.MethodPost, Path: "/v1/intents/{id}/cancel", Tags: []string{"intenções"}, Security: bearer},
		func(ctx context.Context, in *idPath) (*body[intent.Intent], error) {
			buyer, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			it, err := s.d.Market.CancelIntent(ctx, buyer, in.ID)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[intent.Intent]{it}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "submit-proposal", Method: http.MethodPost, Path: "/v1/intents/{id}/proposals", Tags: []string{"propostas"},
		Summary: "Envia uma proposta selada", Security: bearer, DefaultStatus: http.StatusCreated,
	},
		func(ctx context.Context, in *submitInput) (*body[proposal.Proposal], error) {
			seller, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.limit(ctx, "proposals", seller, 5, 10); err != nil {
				return nil, err
			}
			p, err := s.d.Market.SubmitProposal(ctx, seller, in.ID, app.SubmitInput(in.Body))
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[proposal.Proposal]{p}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "revise-proposal", Method: http.MethodPut, Path: "/v1/intents/{id}/proposals/{pid}", Tags: []string{"propostas"}, Security: bearer},
		func(ctx context.Context, in *reviseInput) (*body[proposal.Proposal], error) {
			seller, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.limit(ctx, "proposals", seller, 5, 10); err != nil {
				return nil, err
			}
			p, err := s.d.Market.ReviseProposal(ctx, seller, in.ProposalID, app.SubmitInput(in.Body))
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[proposal.Proposal]{p}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "withdraw-proposal", Method: http.MethodDelete, Path: "/v1/intents/{id}/proposals/{pid}", Tags: []string{"propostas"}, Security: bearer},
		func(ctx context.Context, in *proposalPath) (*body[proposal.Proposal], error) {
			seller, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			p, err := s.d.Market.WithdrawProposal(ctx, seller, in.ProposalID)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[proposal.Proposal]{p}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "list-proposals", Method: http.MethodGet, Path: "/v1/intents/{id}/proposals", Tags: []string{"propostas"},
		Summary: "Comprador vê todas (preço só depois do fechamento); vendedor vê a sua", Security: bearer,
	},
		func(ctx context.Context, in *idPath) (*body[[]app.ProposalView], error) {
			viewer, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			ps, err := s.d.Market.Proposals(ctx, viewer, in.ID)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[[]app.ProposalView]{ps}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "ranking", Method: http.MethodGet, Path: "/v1/intents/{id}/ranking", Tags: []string{"seleção"},
		Summary: "Ordena as propostas reveladas por preço, reputação, aderência e prazo", Security: bearer,
	},
		func(ctx context.Context, in *rankingInput) (*body[[]selection.Ranked], error) {
			buyer, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			rs, err := s.d.Market.Ranking(ctx, buyer, in.ID, selection.Weights{
				Price: in.Price, Reputation: in.Reputation,
				Adherence: in.Adherence, LeadTime: in.LeadTime,
			}, selection.Method(in.Method))
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[[]selection.Ranked]{rs}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "award", Method: http.MethodPost, Path: "/v1/intents/{id}/award", Tags: []string{"seleção"},
		Summary: "Escolhe a proposta e forma o acordo", Security: bearer, DefaultStatus: http.StatusCreated,
	},
		func(ctx context.Context, in *awardInput) (*body[agreement.Agreement], error) {
			buyer, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			a, err := s.d.Market.Award(ctx, buyer, in.ID, in.Body.ProposalID)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[agreement.Agreement]{a}, nil
		})

	agreementOp := func(id, path, summary string, fn func(ctx context.Context, party, agreementID string) (agreement.Agreement, error)) {
		huma.Register(api, huma.Operation{OperationID: id, Method: http.MethodPost, Path: path, Tags: []string{"acordos"}, Summary: summary, Security: bearer},
			func(ctx context.Context, in *idPath) (*body[agreement.Agreement], error) {
				party, err := requireParty(ctx)
				if err != nil {
					return nil, err
				}
				a, err := fn(ctx, party, in.ID)
				if err != nil {
					return nil, s.problem(ctx, err)
				}
				return &body[agreement.Agreement]{a}, nil
			})
	}
	agreementOp("deliver", "/v1/agreements/{id}/deliver", "Comprador confirma o recebimento", s.d.Market.Deliver)
	agreementOp("accept", "/v1/agreements/{id}/accept", "Comprador aceita: pede a liberação ao agente de custódia", s.d.Market.Accept)
	agreementOp("request-refund", "/v1/agreements/{id}/refund", "Comprador pede estorno quando o prazo de envio passa", s.d.Market.RequestRefund)

	huma.Register(api, huma.Operation{OperationID: "get-agreement", Method: http.MethodGet, Path: "/v1/agreements/{id}", Tags: []string{"acordos"}, Security: bearer},
		func(ctx context.Context, in *idPath) (*body[agreement.Agreement], error) {
			party, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			a, err := s.d.Market.Agreement(ctx, party, in.ID)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[agreement.Agreement]{a}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "ship", Method: http.MethodPost, Path: "/v1/agreements/{id}/ship", Tags: []string{"acordos"}, Security: bearer},
		func(ctx context.Context, in *shipInput) (*body[agreement.Agreement], error) {
			seller, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			a, err := s.d.Market.Ship(ctx, seller, in.ID, in.Body.Tracking)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[agreement.Agreement]{a}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "open-dispute", Method: http.MethodPost, Path: "/v1/agreements/{id}/disputes", Tags: []string{"acordos"},
		Summary: "Abre conferência de campos probatórios", Security: bearer,
	},
		func(ctx context.Context, in *disputeInput) (*body[agreement.Agreement], error) {
			buyer, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			a, err := s.d.Market.OpenDispute(ctx, buyer, in.ID, in.Body.Claims)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[agreement.Agreement]{a}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "resolve-dispute", Method: http.MethodPost, Path: "/v1/agreements/{id}/disputes/verdict", Tags: []string{"acordos"},
		Summary: "Árbitro informa os campos confirmados; a regra decide", Security: bearer,
	},
		func(ctx context.Context, in *verdictInput) (*body[agreement.Agreement], error) {
			arbiter, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			a, err := s.d.Market.Resolve(ctx, arbiter, in.ID, in.Body.Confirmed)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[agreement.Agreement]{a}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "rate", Method: http.MethodPost, Path: "/v1/agreements/{id}/ratings", Tags: []string{"reputação"},
		Summary: "Avalia a outra parte, às cegas", Security: bearer, DefaultStatus: http.StatusAccepted,
	},
		func(ctx context.Context, in *rateInput) (*struct{}, error) {
			rater, err := requireParty(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.d.Market.Rate(ctx, rater, in.ID, in.Body.Stars); err != nil {
				return nil, s.problem(ctx, err)
			}
			return &struct{}{}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "reputation", Method: http.MethodGet, Path: "/v1/parties/{id}/reputation", Tags: []string{"reputação"}},
		func(ctx context.Context, in *idPath) (*body[map[reputation.Role]reputation.Score], error) {
			if err := limitRead(ctx, "read"); err != nil {
				return nil, err
			}
			r, err := s.d.Market.Reputation(ctx, in.ID)
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			return &body[map[reputation.Role]reputation.Score]{r}, nil
		})

	if s.d.DevAuth {
		s.devRoutes(api)
	}
}

type registerInput struct {
	Body struct {
		ID          string                `json:"id,omitempty"`
		Name        string                `json:"name"`
		Document    string                `json:"document" doc:"CPF ou CNPJ (alfanumérico aceito)"`
		Credentials []taxonomy.Credential `json:"credentials,omitempty"`
		Arbiter     bool                  `json:"arbiter,omitempty"`
	}
}

type tokenOutput struct {
	Body struct {
		Party app.Party `json:"party"`
		Token string    `json:"token"`
	}
}

// devRoutes register parties and issue tokens; disabled in production.
func (s *server) devRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "dev-register", Method: http.MethodPost, Path: "/v1/dev/parties", Tags: []string{"dev"},
		Summary: "Cadastra uma parte (só em desenvolvimento) e devolve um token", DefaultStatus: http.StatusCreated,
	},
		func(ctx context.Context, in *registerInput) (*tokenOutput, error) {
			p, err := s.d.Market.RegisterParty(ctx, app.RegisterPartyInput(in.Body))
			if err != nil {
				return nil, s.problem(ctx, err)
			}
			tok, err := s.issue(p.ID, 24*time.Hour)
			if err != nil {
				return nil, err
			}
			out := &tokenOutput{}
			out.Body.Party, out.Body.Token = p, tok
			return out, nil
		})
}
