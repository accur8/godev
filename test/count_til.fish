#!/usr/bin/env fish


set ct 1
set max $argv[1]

if test "$max" = ""
  set max 999999999
end

echo counting to $max

while test $ct -le $max
  sleep 1 
  echo $ct (date)
  set ct (math $ct+1)
end

exit 10
