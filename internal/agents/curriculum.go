// Package agents implements the eight specialist agents that make up the
// exam-prep system. Each agent is a struct with a method that takes a typed
// input and returns a typed output, suitable for wrapping with channels.Map.
package agents

import (
	"math/rand"

	"examen/internal/types"
)

// Curriculum agent — picks the next topic based on the student's profile.
// In a real system this would do spaced repetition; the hackathon version
// uses a weighted random pick favoring weak skills.
type Curriculum struct {
	rng     *rand.Rand
	catalog map[types.Subject][]types.Topic
}

func NewCurriculum(seed int64) *Curriculum {
	return &Curriculum{
		rng:     rand.New(rand.NewSource(seed)),
		catalog: defaultCatalog(),
	}
}

func defaultCatalog() map[types.Subject][]types.Topic {
	return map[types.Subject][]types.Topic{
		types.SubjectGeometry: {
			{Subject: types.SubjectGeometry, Name: "right_triangle_trig",
				Display: "Right Triangle Trigonometry",
				Skills:  []string{"trig.tan", "trig.sin", "trig.cos", "angle_of_elevation"},
				Difficulty: 3},
			{Subject: types.SubjectGeometry, Name: "circle_theorems",
				Display: "Circle Theorems",
				Skills:  []string{"circles.inscribed_angle", "circles.central_angle"},
				Difficulty: 3},
			{Subject: types.SubjectGeometry, Name: "similar_triangles",
				Display: "Similar Triangles",
				Skills:  []string{"similar_triangles.ratio", "scale_factor"},
				Difficulty: 2},
		},
		types.SubjectAlgebra: {
			{Subject: types.SubjectAlgebra, Name: "linear_equations",
				Display: "Linear Equations",
				Skills:  []string{"algebra.linear", "isolation", "like_terms"},
				Difficulty: 2},
			{Subject: types.SubjectAlgebra, Name: "quadratic_equations",
				Display: "Quadratic Equations",
				Skills:  []string{"algebra.quadratic", "factoring"},
				Difficulty: 3},
			{Subject: types.SubjectAlgebra, Name: "systems_of_equations",
				Display: "Systems of Equations",
				Skills:  []string{"algebra.systems", "elimination", "substitution"},
				Difficulty: 3},
		},
	}
}

// Next picks a topic for the student. This is what gets wrapped by Map:
//
//	pickTopic := channels.Map(curriculum.Next)
func (c *Curriculum) Next(profile types.StudentProfile) types.TopicRequest {
	// Decide subject — student's choice or alternate
	subj := profile.Subject
	if subj == "" {
		if c.rng.Intn(2) == 0 {
			subj = types.SubjectGeometry
		} else {
			subj = types.SubjectAlgebra
		}
	}

	topics := c.catalog[subj]

	// If we have weak skills, prefer topics that cover them
	if len(profile.WeakSkills) > 0 {
		for _, t := range topics {
			for _, ws := range profile.WeakSkills {
				for _, ts := range t.Skills {
					if ts == ws {
						return types.TopicRequest{
							StudentID:  profile.StudentID,
							Topic:      t,
							Difficulty: t.Difficulty,
							Skills:     t.Skills,
							Avoid:      profile.SeenIDs,
						}
					}
				}
			}
		}
	}

	t := topics[c.rng.Intn(len(topics))]
	return types.TopicRequest{
		StudentID:  profile.StudentID,
		Topic:      t,
		Difficulty: t.Difficulty,
		Skills:     t.Skills,
		Avoid:      profile.SeenIDs,
	}
}
