package d2l

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// Instructor settings that Valence exposes on forums and topics. Checking
// them before a write turns Brightspace's bare 403 into a specific reason.
// Role permissions such as "edit own posts" are not exposed, so edits and
// deletes still rely on Brightspace's answer.

// Topic RatingType (RATING_T).
const (
	ratingNone     = 0
	ratingFiveStar = 1
)

type discussionRules struct {
	forum, topic Object
}

func (c *Client) discussionRules(ctx context.Context, t PostTarget) (discussionRules, error) {
	var rules discussionRules
	forum := c.LE(fmt.Sprintf("/%d/discussions/forums/%d", t.CourseID, t.ForumID))
	if err := c.Get(ctx, forum, nil, &rules.forum); err != nil {
		return rules, err
	}
	if err := c.Get(ctx, forum+"/topics/"+strconv.FormatInt(t.TopicID, 10), nil, &rules.topic); err != nil {
		return rules, err
	}
	return rules, nil
}

// postingBlocked returns why a new post would be refused, or "" when the
// visible settings allow it.
func (r discussionRules) postingBlocked(now time.Time, anonymous bool) string {
	name := stringField(r.topic, "Name")
	switch {
	case r.forum["IsLocked"] == true:
		return "The forum is locked by the instructor; it accepts no new posts."
	case r.topic["IsLocked"] == true:
		return fmt.Sprintf("The topic %q is locked by the instructor; it accepts no new posts.", name)
	case r.forum["IsHidden"] == true || r.topic["IsHidden"] == true:
		return fmt.Sprintf("The topic %q is hidden from students.", name)
	case r.topic["GroupTypeId"] != nil:
		return fmt.Sprintf("The topic %q is a group topic; the Valence API cannot post to group topics. Use the Brightspace web interface.", name)
	case anonymous && r.topic["AllowAnonymousPosts"] != true:
		return fmt.Sprintf("The topic %q does not allow anonymous posts.", name)
	}

	windows := []struct {
		what   string
		object Object
		start  string
		end    string
	}{
		{"topic", r.topic, "StartDate", "EndDate"},
		{"forum", r.forum, "StartDate", "EndDate"},
		{"forum posting period", r.forum, "PostStartDate", "PostEndDate"},
	}
	for _, w := range windows {
		if reason := outsideWindow(now, w.what, timeField(w.object, w.start), timeField(w.object, w.end)); reason != "" {
			return reason
		}
	}

	return ""
}

// ratingBlocked returns why the user cannot give the post a star rating.
func (r discussionRules) ratingBlocked(post Object) string {
	switch int64Field(r.topic, "RatingType") {
	case ratingFiveStar:
	case ratingNone:
		return "Ratings are turned off for this topic."
	default:
		return "This topic uses up/down votes, not star ratings."
	}
	if post["CanRate"] == false {
		return "Brightspace reports that you cannot rate this post (for example, your own post)."
	}
	return ""
}

// checkRating reads the topic and post so a refused rating names its cause.
func (c *Client) checkRating(ctx context.Context, t PostTarget, postID string) error {
	if !c.writes {
		return writesDisabled()
	}
	rules, err := c.discussionRules(ctx, t)
	if err != nil {
		return err
	}
	var post Object
	if err := c.Get(ctx, t.path(c, postID), nil, &post); err != nil {
		return err
	}
	if reason := rules.ratingBlocked(post); reason != "" {
		return notAllowed(reason)
	}
	return nil
}

func outsideWindow(now time.Time, what string, start, end time.Time) string {
	switch {
	case !start.IsZero() && now.Before(start):
		return fmt.Sprintf("The %s opens %s; posting is not allowed yet.", what, FormatTime(start))
	case !end.IsZero() && now.After(end):
		return fmt.Sprintf("The %s closed %s; posting is no longer allowed.", what, FormatTime(end))
	}
	return ""
}

func notAllowed(reason string) error {
	return &Error{
		Code:     "NOT_ALLOWED",
		Message:  reason,
		NextStep: "Nothing was sent. Tell the user; this is a course setting, not an error to retry.",
	}
}
