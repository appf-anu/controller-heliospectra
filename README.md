# controller-heliospectra

this repo is deprecated, old and archived, new repo ishere https://gitlab.com/appf-anu/controller-heliospectra2

go controller for heliospectra s7 and s10 lights

*multiplier* param and *MULTIPLIER* environment variable are used to change the value sent to the light

channels are sequentially numbered as such in conditions file:


### Heliospectra S7
| channel | wavelength  |
|---      | --          |
|channel-1| 400nm       |
|channel-2| 420nm       |
|channel-3| 450nm       |
|channel-4| 530nm       |
|channel-5| 630nm       |
|channel-6| 660nm       |
|channel-7| 735nm       |


### Heliospectra S10
| channel   | wavelength|
|---        | --        |
|channel-1  | 370nm     |
|channel-2  | 400nm     |
|channel-3  | 420nm     |
|channel-4  | 450nm     |
|channel-5  | 530nm     |
|channel-6  | 620nm     |
|channel-7  | 660nm     |
|channel-8  | 735nm     |
|channel-9  | 850nm     |
|channel-10 | 6500k     |

## State of heliospectra lights and their HTTP "API"

Right now this utility uses telnet to control the heliospectras, however I'm looking at using the HTTP "api" that I've been able to decode.

to set intensity make a GET request (I know) to this url:

`http://<ip address>/intensity.cgi?int=1000:1000:1000:1000:1000:1000:1000`

AFAICT the values are just <int(channel_intensity)> joined by ":" char


for light status (I know) make a GET request to this url:

`http://<ip address>/status.xml`

You should get a response like this for an S7 with all channels on max:

```xml
<r>
<a>2020:02:23:13:48:08</a>
<b>Running</b>
<c>OK</c>
<d>6d 00h 05m 27s</d>
<e>2020-02-23   13:40:41</e>
<f>Web</f>
<g>192.168.1.200</g>
<h>Light setting</h>
<i>0:43.5C,1:40.5C,2:44.8C,3:40.3C,</i>
<j>0:1000,1:1000,2:1000,3:1000,4:1000,5:1000,6:1000,</j>
<k>0|^|239.64.10.253|^||~|1|^|239.63.247.177|^||~|2|^|239.63.251.225|^||~|</k>
<l> </l>
<m>Master</m>
<n>C:on</n>
<o>off:Enter your message here:heliospectra</o>
<p> </p>
<q>on, pool.ntp.org, -10:00:00</q>
<s>on</s>
<r></r>
<t></t>
</r>
```

this is a response for an S10 with all lights on max:

```xml
<r>
<a>2020:02:23:23:56:03</a>
<b>Not running</b>
<c>OK</c>
<d>6d 00h 13m 43s</d>
<e>2020-02-23   23:50:00</e>
<f>External (TCP)</f>
<g>192.168.1.200</g>
<h>Light setting</h>
<i>0:31.3C,1:30.8C,2:31.8C,3:30.8C,</i>
<j>0:1000,1:1000,2:1000,3:1000,4:1000,5:1000,6:1000,7:1000,8:1000,9:1000,</j>
<k>0|^|239.64.22.58|^||~|</k>
<l> </l>
<m>Master</m>
<n>C:on</n>
<o>off:Enter your message here:heliospectra</o>
<p> </p>
<q>on, pool.ntp.org, 00:00:00</q>
<s>on</s>
<r></r>
<t></t>
</r>

```

These responses look like XML and they are sent with the 'text/xml' Content-Type header but they dont follow the XML standard at all.

My understanding of the data provided is this:

|tag    |meaning                            |example values                  |
|---    |---                                |---                             |
|r      |wrapper value                      |                                |
|a      |current time according to the light|2020:02:23:23:56:03             |
|b      |state of the schedule              |Not running                     |
|c      |the "Status" of the light          |OK                              |
|d      |light uptime                       |6d 00h 13m 43s                  |
|e      |time of last change                |2020-02-23\t23:50:00            |
|f      |Last control changed by            |External (UDP)                  |
|g      |last changed by ip address         |192.168.1.200                   |
|h      |the type of change                 |Light setting                   |
|i      |light plate temperatures           |0:43.5C,1:40.6C,2:45.0C,3:40.5C,|
|j      |light intensity values             |0:1000,1:1000,2:1000,3:1000,4:1000,5:1000,6:1000,7:1000,8:1000,9:1000,|
|k      |something about the Master         | 0|^|239.64.22.58|^||~|         |
|l      |?                                  |                                |
|m      |Runnning mode                      |Master                          |
|n      |?                                  |C:on                            |
|o      |?                                  |off:Enter your message here:heliospectra |
|p      |?                                  |                                |
|q      |ntp info                           |on, pool.ntp.org, 00:00:00      |
|s      |?                                  |on                              |
|r      |?                                  |                                |
|t      |?                                  |                                |
