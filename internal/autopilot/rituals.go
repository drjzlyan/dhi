package autopilot

import "fmt"

// Ritual is a ready-made autopilot that keeps a self-running team honest
// (F-071): a daily standup per team, a weekly self-review per agent.
type Ritual struct {
	Slug, Name, Agent, Prompt, Schedule string
}

// Rituals returns the standups for teams led by an agent (lead per team
// slug) and the weekly self-review for each agent, in a stable order.
func Rituals(leads map[string]string, teams []string, agents []string) []Ritual {
	var out []Ritual
	for _, team := range teams {
		lead := leads[team]
		if lead == "" {
			continue
		}
		out = append(out, Ritual{
			Slug: "standup-" + team, Name: "Standup · " + team, Agent: lead, Schedule: "daily 09:30",
			Prompt: fmt.Sprintf("Daily standup for team %s. Use task_list and channel_read to report, per member: what "+
				"finished since yesterday, what is in progress, and what is blocked (on what). Post it in #%s. Mention @you "+
				"only for a blocker that needs the human; @mention a member only to unblock them.", team, team),
		})
	}
	for _, a := range agents {
		out = append(out, Ritual{
			Slug: "review-" + a, Name: "Weekly review · " + a, Agent: a, Schedule: "weekly fri 17:00",
			Prompt: "Weekly self-review. List the tasks you finished this week, what failed or needed fixing and why, " +
				"and the lessons you hold. Record anything durable you have not yet (lesson_record), propose a tool for work " +
				"you keep repeating (tool_propose) and a setting that kept getting in your way (settings_propose). " +
				"End with one thing you will do differently next week. Address the summary to @you.",
		})
	}
	return out
}

// AddRituals creates every ritual not already present, paused so the
// human arms each one deliberately. It returns how many it added.
func (s *Store) AddRituals(rs []Ritual) (int, error) {
	n := 0
	for _, r := range rs {
		if _, exists := s.Get(r.Slug); exists {
			continue
		}
		sch, err := ParseSchedule(r.Schedule)
		if err != nil {
			return n, err
		}
		if _, err := s.Create(r.Slug, r.Name, r.Agent, r.Prompt, sch); err != nil {
			return n, err
		}
		if err := s.SetEnabled(r.Slug, false); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
