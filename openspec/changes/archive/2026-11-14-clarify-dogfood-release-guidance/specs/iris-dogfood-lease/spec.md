## ADDED Requirements

### Requirement: Plain dogfood release guidance

Iris SHALL explicitly tell agents not to hold dogfood for a user who is away; not to hold it while fixing a problem they found, but to release it, fix the problem, and get back in line with a new claim; and to release it right away when the user is away and Iris asks for it back or says someone is waiting.

This guidance SHALL appear in granted `iris_dogfood_claim` results, successful `iris_set_dogfood` results, grant notices, holder check-ins and final release warnings, the descriptions of `iris_set_dogfood`, `iris_dogfood_claim`, and `iris_dogfood_renew` (including their CLI help), and the shipped Iris skill documentation. This change SHALL affect guidance only, leaving lease enforcement, timers, and queue ordering unchanged.

#### Scenario: Agent receives dogfood

- **WHEN** a claim returns granted, including a repeated claim by the holder, or a grant notice is sent
- **THEN** its message explicitly states all three release rules

#### Scenario: Deployment succeeds

- **WHEN** `iris_set_dogfood` succeeds
- **THEN** its result includes a message explicitly stating all three release rules

#### Scenario: Holder is asked to respond

- **WHEN** Iris sends a holder check-in, a someone-is-waiting notice, or a final release warning
- **THEN** the notice explicitly states all three rules, including immediate release when the user is away

#### Scenario: Agent reads tool and skill instructions

- **WHEN** an agent reads a dogfood claim, renewal, or deployment tool description or the shipped Iris skill
- **THEN** the instructions explicitly state all three release rules
