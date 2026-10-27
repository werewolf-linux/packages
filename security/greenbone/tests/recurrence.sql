-- Exercise both libical 4's owned recurrence rules and its counted BY arrays.
-- Fixed UTC expectations also catch accidental changes to previous occurrences,
-- exclusions, empty rules and timezone transitions.
DO $$
DECLARE
    sample record;
    calendar text;
    actual bigint;
    expected bigint;
BEGIN
    FOR sample IN SELECT * FROM (VALUES
        ('daily', 'DTSTART:20260101T090000Z', 'RRULE:FREQ=DAILY',
         '2026-01-03 12:00:00+00', 0, '2026-01-04 09:00:00+00'),
        ('previous', 'DTSTART:20260101T090000Z', 'RRULE:FREQ=DAILY',
         '2026-01-03 12:00:00+00', -1, '2026-01-03 09:00:00+00'),
        ('hourly interval', 'DTSTART:20260101T090000Z', 'RRULE:FREQ=HOURLY;INTERVAL=2',
         '2026-01-03 12:00:00+00', 0, '2026-01-03 13:00:00+00'),
        ('BYDAY', 'DTSTART:20260101T090000Z', 'RRULE:FREQ=WEEKLY;BYDAY=MO,WE',
         '2026-01-05 10:00:00+00', 0, '2026-01-07 09:00:00+00'),
        ('exclusion', 'DTSTART:20260101T090000Z', E'RRULE:FREQ=DAILY\nEXDATE:20260104T090000Z',
         '2026-01-03 12:00:00+00', 0, '2026-01-05 09:00:00+00'),
        ('expired count', 'DTSTART:20260101T090000Z', 'RRULE:FREQ=DAILY;COUNT=3',
         '2026-01-05 12:00:00+00', 0, NULL),
        ('last counted occurrence', 'DTSTART:20260101T090000Z', 'RRULE:FREQ=DAILY;COUNT=3',
         '2026-01-05 12:00:00+00', -1, '2026-01-03 09:00:00+00'),
        ('single event', 'DTSTART:20260101T090000Z', '',
         '2025-12-31 12:00:00+00', 0, '2026-01-01 09:00:00+00'),
        ('DST', 'DTSTART;TZID=Europe/Berlin:20260328T090000', 'RRULE:FREQ=DAILY',
         '2026-03-28 12:00:00+00', 0, '2026-03-29 07:00:00+00')
    ) AS cases(name, starts, rule, reference_time, periods_offset, expected_time)
    LOOP
        calendar := format(E'BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:werewolf-test\n%s\n%s\nEND:VEVENT\nEND:VCALENDAR',
                           sample.starts, sample.rule);
        actual := next_time_ical(calendar,
                                extract(epoch FROM sample.reference_time::timestamptz)::bigint,
                                'UTC', sample.periods_offset);
        expected := coalesce(extract(epoch FROM sample.expected_time::timestamptz)::bigint, 0);
        IF actual IS DISTINCT FROM expected THEN
            RAISE EXCEPTION '%: expected %, got %', sample.name, expected, actual;
        END IF;
    END LOOP;
END;
$$;
